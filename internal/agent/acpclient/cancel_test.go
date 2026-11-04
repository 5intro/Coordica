package acpclient

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

type cancelPromptOutcome struct {
	result PromptResult
	err    error
}

func cancelFixture(t *testing.T, scenario string) (*Peer, Session, string) {
	t.Helper()
	config, journal := fixtureConfig(t, "cancel-"+scenario)
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

func startCancelable(s Session, ctx context.Context, text string, signal <-chan struct{}) <-chan cancelPromptOutcome {
	done := make(chan cancelPromptOutcome, 1)
	go func() { r, err := s.PromptTextWithCancel(ctx, text, signal); done <- cancelPromptOutcome{r, err} }()
	return done
}

func awaitCancelable(t *testing.T, done <-chan cancelPromptOutcome) cancelPromptOutcome {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(10 * time.Second):
		t.Fatal("cancelable prompt did not complete or reap writers")
		return cancelPromptOutcome{}
	}
}

func awaitCancelFrame(t *testing.T, journal string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		raw, _ := os.ReadFile(journal)
		if strings.Contains(string(raw), "session/cancel") {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("fixture did not receive cancellation notification")
}

func TestCancelNotificationUsesOriginalTerminalAndRetainsPeer(t *testing.T) {
	p, s, journal := cancelFixture(t, "confirmed")
	signal := make(chan struct{}, 3)
	done := startCancelable(s, t.Context(), "explicit-held-work", signal)
	awaitUpdates(t, s, 2)
	if _, err := s.PromptText(t.Context(), "must-not-queue"); !errors.Is(err, ErrPromptBusy) {
		t.Fatal(err)
	}
	if _, err := s.SetConfigOption(t.Context(), "not-selected", "value"); !errors.Is(err, ErrSessionBusy) {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		signal <- struct{}{}
	}
	out := awaitCancelable(t, done)
	if out.err != nil || out.result.LocalRequestID != 3 || out.result.Evidence != "rpc_terminal" || out.result.StopReason != "cancelled" || p.Err() != nil {
		t.Fatal(out, p.Err())
	}
	updates := awaitUpdates(t, s, 3)
	if updates.Gap || updates.Process.TreeStopped || updates.Facts[2].Kind != "tool_call_update" {
		t.Fatal("cancel lost late updates or claimed OS stop", updates)
	}
	next, err := s.PromptText(t.Context(), "after-cancel")
	if err != nil || next.LocalRequestID != 4 || next.StopReason != "end_turn" {
		t.Fatal("cancel implicitly ended Session or consumed a request ID", next, err)
	}
	if status, _ := p.Close(); !status.TreeStopped {
		t.Fatal("local fixture process not stopped")
	}
	raw, _ := os.ReadFile(journal)
	if strings.Count(string(raw), "session/cancel") != 1 || strings.Count(string(raw), "session/prompt") != 2 || strings.Contains(string(raw), "session/set_config_option") {
		t.Fatal("duplicate signal produced another side effect", string(raw))
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var fields map[string]json.RawMessage
		if json.Unmarshal([]byte(line), &fields) != nil {
			t.Fatal("invalid journal frame")
		}
		if string(fields["method"]) == `"session/cancel"` {
			if _, exists := fields["id"]; exists || len(fields) != 3 {
				t.Fatal("cancel is not an ID-less notification", string(line))
			}
		}
	}
}

func TestCancelPreSignalAndLateSignalNeverStartOrRetargetWork(t *testing.T) {
	p, s, journal := cancelFixture(t, "natural")
	pre := make(chan struct{})
	close(pre)
	result, err := s.PromptTextWithCancel(t.Context(), "must-not-start", pre)
	if !errors.Is(err, context.Canceled) || result.Evidence != "" || p.Err() != nil {
		t.Fatal(result, err, p.Err())
	}
	late := make(chan struct{})
	result, err = s.PromptTextWithCancel(t.Context(), "finished-before-signal", late)
	if err != nil || result.LocalRequestID != 3 || result.StopReason != "end_turn" {
		t.Fatal(result, err)
	}
	close(late)
	result, err = s.PromptText(t.Context(), "after-cancel")
	if err != nil || result.LocalRequestID != 4 || result.StopReason != "end_turn" {
		t.Fatal(result, err)
	}
	p.Close()
	raw, _ := os.ReadFile(journal)
	if strings.Contains(string(raw), "session/cancel") || strings.Count(string(raw), "session/prompt") != 2 || strings.Contains(string(raw), "must-not-start") {
		t.Fatal("signal survived its invocation or consumed a request ID", string(raw))
	}
}

func TestCancelOtherTerminalOrRejectionIsPreservedAndPeerRetired(t *testing.T) {
	for _, scenario := range []string{"other-terminal", "rejected"} {
		t.Run(scenario, func(t *testing.T) {
			p, s, journal := cancelFixture(t, scenario)
			signal := make(chan struct{})
			done := startCancelable(s, t.Context(), "held", signal)
			awaitUpdates(t, s, 2)
			close(signal)
			out := awaitCancelable(t, done)
			var failure *PromptError
			if !errors.Is(out.err, ErrCancelUnconfirmed) || errors.Is(out.err, ErrOutcomeUnknown) || !errors.As(out.err, &failure) || !failure.Process.TreeStopped || p.Err() == nil {
				t.Fatal("non-cancelled terminal lost fact or retirement", out)
			}
			if scenario == "other-terminal" {
				if out.result.StopReason != "end_turn" || out.result.Evidence != "rpc_terminal" {
					t.Fatal(out)
				}
			} else {
				var rpc *RPCError
				if !errors.As(out.err, &rpc) || strings.Contains(out.err.Error(), "private-") {
					t.Fatal("native rejection lost or leaked", out)
				}
			}
			if _, err := s.PromptText(t.Context(), "must-not-follow"); !errors.Is(err, ErrDisconnected) {
				t.Fatal(err)
			}
			raw, _ := os.ReadFile(journal)
			if strings.Count(string(raw), "session/cancel") != 1 || strings.Count(string(raw), "session/prompt") != 1 {
				t.Fatal(string(raw))
			}
		})
	}
}

func TestCancelNotificationWriteIsNotActualStopProof(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(map[bool]string{false: "context_cancel", true: "deadline"}[deadline], func(t *testing.T) {
			p, s, journal := cancelFixture(t, "no-terminal")
			ctx, stop := context.WithCancel(t.Context())
			if deadline {
				stop()
				ctx, stop = context.WithTimeout(t.Context(), 2*time.Second)
			}
			defer stop()
			signal := make(chan struct{})
			done := startCancelable(s, ctx, "unconfirmed", signal)
			awaitUpdates(t, s, 2)
			close(signal)
			awaitCancelFrame(t, journal)
			select {
			case out := <-done:
				t.Fatal("notification delivery was mistaken for stop", out)
			default:
			}
			updates, _ := s.Updates()
			if p.Err() != nil || updates.Process.TreeStopped {
				t.Fatal("send guessed failure or process stop", p.Err(), updates)
			}
			if !deadline {
				stop()
			}
			out := awaitCancelable(t, done)
			var failure *PromptError
			if out.result.Evidence != "" || out.result.StopReason != "" || !errors.Is(out.err, ErrOutcomeUnknown) || !errors.As(out.err, &failure) || !failure.Process.TreeStopped {
				t.Fatal("unconfirmed prompt became native cancelled", out)
			}
			if deadline && !errors.Is(out.err, context.DeadlineExceeded) {
				t.Fatal("cancel changed the original deadline", out.err)
			}
		})
	}
}

func TestCancelUncertainWireNeverGuessesCancelledOrRetries(t *testing.T) {
	for _, scenario := range []string{"disconnect", "wrong-id", "ack-only"} {
		t.Run(scenario, func(t *testing.T) {
			_, s, journal := cancelFixture(t, scenario)
			signal := make(chan struct{})
			done := startCancelable(s, t.Context(), "held", signal)
			awaitUpdates(t, s, 2)
			close(signal)
			out := awaitCancelable(t, done)
			var failure *PromptError
			if out.result.StopReason != "" || !errors.Is(out.err, ErrOutcomeUnknown) || !errors.As(out.err, &failure) || !failure.Process.TreeStopped {
				t.Fatal(out)
			}
			raw, _ := os.ReadFile(journal)
			if strings.Count(string(raw), "session/cancel") != 1 || strings.Count(string(raw), "session/prompt") != 1 {
				t.Fatal("uncertainty retried native work", string(raw))
			}
		})
	}
}
