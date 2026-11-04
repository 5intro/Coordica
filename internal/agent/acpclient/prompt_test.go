package acpclient

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPromptTextTwoExplicitTurnsKeepNativeTerminalAndSessionMetadata(t *testing.T) {
	config, journal := fixtureConfig(t, "prompt-normal")
	p, err := Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	s, err := p.NewSession(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for i, text := range []string{"explicit-work-one", "explicit-work-two"} {
		result, err := s.PromptText(t.Context(), text)
		if err != nil || result.Evidence != "rpc_terminal" || result.StopReason != "end_turn" || result.LocalRequestID != uint64(i+3) || p.Err() != nil {
			t.Fatal(result, err, p.Err())
		}
		raw, _ := json.Marshal(result)
		if strings.Contains(string(raw), "private-") || strings.Contains(string(raw), "explicit-work") {
			t.Fatal("receipt contains input/output body")
		}
	}
	updates := awaitUpdates(t, s, 4)
	if updates.Gap || len(updates.Facts) != 4 || updates.Process.TreeStopped {
		t.Fatal("terminal erased Session updates or invented tree stop", updates)
	}
	raw, err := os.ReadFile(journal)
	if err != nil || strings.Count(string(raw), "session/prompt") != 2 || strings.Count(string(raw), "session/new") != 1 || !strings.Contains(string(raw), "explicit-work-one") || !strings.Contains(string(raw), "explicit-work-two") {
		t.Fatalf("explicit native work journal: %s %v", raw, err)
	}
}

func promptFixture(t *testing.T, scenario string) (*Peer, Session, string) {
	t.Helper()
	config, journal := fixtureConfig(t, "prompt-"+scenario)
	p, err := Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	s, err := p.NewSession(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return p, s, journal
}

func TestPromptTextPreservesAllNativeStopReasonsAndKnownRejection(t *testing.T) {
	p, s, _ := promptFixture(t, "normal")
	for i, reason := range []string{"end_turn", "max_tokens", "max_turn_requests", "refusal", "cancelled"} {
		result, err := s.PromptText(t.Context(), "reason:"+reason)
		if err != nil || result.StopReason != reason || result.LocalRequestID != uint64(i+3) || result.Evidence != "rpc_terminal" || p.Err() != nil {
			t.Fatal(result, err)
		}
	}
	p, s, journal := promptFixture(t, "rejected")
	_, err := s.PromptText(t.Context(), "reject")
	var rpc *RPCError
	if !errors.As(err, &rpc) || errors.Is(err, ErrOutcomeUnknown) || p.Err() != nil || strings.Contains(err.Error(), "private-") {
		t.Fatal("native rejection lost classification", err)
	}
	result, err := s.PromptText(t.Context(), "explicit-after-rejection")
	if err != nil || result.LocalRequestID != 4 || result.StopReason != "end_turn" {
		t.Fatal(result, err)
	}
	raw, _ := os.ReadFile(journal)
	if strings.Count(string(raw), "session/prompt") != 2 || strings.Contains(string(raw), "authenticate") {
		t.Fatal("rejection retried/authenticated automatically")
	}
}

func TestPromptTextWaitsForTerminalAndRejectsConcurrentOrUnsendableInput(t *testing.T) {
	p, s, journal := promptFixture(t, "pause")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.PromptText(ctx, "cancelled-before-send"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, text := range []string{"", string([]byte{255}), strings.Repeat("x", 65537), strings.Repeat("\x00", 65536)} {
		if _, err := s.PromptText(t.Context(), text); !errors.Is(err, ErrInvalidPrompt) || p.Err() != nil {
			t.Fatal("invalid/escaped oversized input damaged Peer", err)
		}
	}
	done := make(chan error, 1)
	go func() { _, err := s.PromptText(t.Context(), "explicit-held-work"); done <- err }()
	awaitUpdates(t, s, 2)
	select {
	case err := <-done:
		t.Fatal("updates were mistaken for terminal ACK", err)
	default:
	}
	if _, err := s.PromptText(t.Context(), "must-not-queue"); !errors.Is(err, ErrPromptBusy) || p.Err() != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(journal), "continue"), []byte("continue"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	metadata := awaitUpdates(t, s, 3)
	if len(metadata.Facts) != 3 || metadata.Facts[2].Sequence != 3 {
		t.Fatal("late Session metadata lost or guessed a Prompt target", metadata)
	}
	raw, _ := os.ReadFile(journal)
	if strings.Count(string(raw), "session/prompt") != 1 {
		t.Fatal("busy/invalid/pre-cancel produced native work")
	}
}

func TestPromptTextCanWaitPastInitializationACKDeadline(t *testing.T) {
	_, s, _ := promptFixture(t, "long")
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	result, err := s.PromptText(ctx, "explicit-long-work")
	if err != nil || result.StopReason != "end_turn" {
		t.Fatal("whole-turn request used a short ACK timeout", result, err)
	}
}

func TestPromptTextUnknownWireNeverRetriesAndReturnsActualStop(t *testing.T) {
	for _, scenario := range []string{"null-result", "null-stop", "missing-stop", "unknown-stop", "wrong-id", "old-id", "duplicate", "disconnect", "permission", "overflow"} {
		t.Run(scenario, func(t *testing.T) {
			p, s, journal := promptFixture(t, scenario)
			expected := 1
			if scenario == "old-id" {
				if _, err := s.PromptText(t.Context(), "first-explicit"); err != nil {
					t.Fatal(err)
				}
				expected = 2
			}
			result, err := s.PromptText(t.Context(), "explicit-uncertain-work")
			if scenario == "duplicate" && result.Evidence == "rpc_terminal" {
				select {
				case <-p.Done():
				case <-time.After(3 * time.Second):
					t.Fatal("duplicate terminal did not fence transport")
				}
				if status, _ := p.Close(); !status.TreeStopped {
					t.Fatal("duplicate did not actually stop")
				}
			} else {
				var failure *PromptError
				if !errors.Is(err, ErrOutcomeUnknown) || !errors.As(err, &failure) || !failure.Process.TreeStopped || result.StopReason != "" {
					t.Fatal("unknown work lost uncertainty/stop evidence", result, err)
				}
			}
			if err != nil && strings.Contains(err.Error(), "private-") {
				t.Fatal("private native payload entered error text")
			}
			if _, err := s.PromptText(t.Context(), "must-not-retry"); !errors.Is(err, ErrDisconnected) {
				t.Fatal("dead Peer accepted another prompt", err)
			}
			raw, _ := os.ReadFile(journal)
			if strings.Count(string(raw), "session/prompt") != expected || strings.Count(string(raw), "session/new") != 1 || strings.Contains(string(raw), "session/cancel") || strings.Contains(string(raw), "authenticate") {
				t.Fatalf("automatic native side effect: %s", raw)
			}
		})
	}
}

func TestPromptTextSentCancellationAndDeadlineAreUnknownNotNativeCancelled(t *testing.T) {
	for _, scenario := range []string{"cancel", "timeout"} {
		t.Run(scenario, func(t *testing.T) {
			_, s, journal := promptFixture(t, "no-response")
			var ctx context.Context
			var cancel context.CancelFunc
			if scenario == "timeout" {
				ctx, cancel = context.WithTimeout(t.Context(), 100*time.Millisecond)
			} else {
				ctx, cancel = context.WithCancel(t.Context())
			}
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := s.PromptText(ctx, "explicit-unconfirmed"); done <- err }()
			awaitUpdates(t, s, 2)
			if scenario == "cancel" {
				cancel()
			}
			err := <-done
			var failure *PromptError
			if !errors.Is(err, ErrOutcomeUnknown) || !errors.As(err, &failure) || !failure.Process.TreeStopped {
				t.Fatal(err)
			}
			if _, err = s.PromptText(t.Context(), "no-retry"); !errors.Is(err, ErrDisconnected) {
				t.Fatal(err)
			}
			raw, _ := os.ReadFile(journal)
			if strings.Count(string(raw), "session/prompt") != 1 || strings.Contains(string(raw), "session/cancel") {
				t.Fatal("ctx cancellation guessed native cancellation")
			}
		})
	}
}

func TestPromptTextKnownTerminalSurvivesCancellationWhileWriterBlocked(t *testing.T) {
	p, s, journal := promptFixture(t, "blocked-writer")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type outcome struct {
		result PromptResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() { r, err := s.PromptText(ctx, strings.Repeat("\"", 65536)); done <- outcome{r, err} }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		p.mu.Lock()
		claimed := p.prompt != nil
		p.mu.Unlock()
		if claimed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("prompt not claimed")
		}
		time.Sleep(time.Millisecond)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(journal), "continue"), []byte("continue"), 0600); err != nil {
		t.Fatal(err)
	}
	for {
		p.mu.Lock()
		ready := p.prompt != nil && p.prompt.ready
		p.mu.Unlock()
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("terminal observation not saved")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case result := <-done:
		t.Fatal("singleflight released before writer was reaped", result)
	default:
	}
	cancel()
	result := <-done
	var failure *PromptError
	if result.result.StopReason != "end_turn" || result.result.Evidence != "rpc_terminal" || errors.Is(result.err, ErrOutcomeUnknown) || !errors.Is(result.err, ErrDisconnected) || !errors.As(result.err, &failure) || !failure.Process.TreeStopped {
		t.Fatal("known terminal lost in ctx/transport race", result)
	}
}
