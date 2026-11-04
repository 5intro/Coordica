package acpclient

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"coordica/internal/process"
)

func awaitCancelState(t *testing.T, p *Peer, condition func(*pendingPrompt) bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		ready := p.prompt != nil && condition(p.prompt)
		p.mu.Unlock()
		if ready {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("prompt/cancel race did not reach its barrier")
}

func TestCancelCannotOvertakeBlockedPromptOrOutliveItsInvocation(t *testing.T) {
	p, s, journal := promptFixture(t, "blocked-writer")
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	signal := make(chan struct{})
	done := startCancelable(s, ctx, strings.Repeat("\"", 65536), signal)
	awaitCancelState(t, p, func(p *pendingPrompt) bool { return true })
	close(signal)
	awaitCancelState(t, p, func(p *pendingPrompt) bool { return p.cancelWriteDone != nil })
	if err := os.WriteFile(filepath.Join(filepath.Dir(journal), "continue"), []byte("continue"), 0600); err != nil {
		t.Fatal(err)
	}
	awaitCancelState(t, p, func(p *pendingPrompt) bool { return p.ready })
	p.mu.Lock()
	attempted := p.prompt.cancelAttempted
	p.mu.Unlock()
	if attempted {
		t.Fatal("cancel notification overtook the blocked original prompt")
	}
	select {
	case out := <-done:
		t.Fatal("prompt retired while a writer was blocked", out)
	default:
	}
	stop()
	out := awaitCancelable(t, done)
	var failure *PromptError
	if out.result.StopReason != "end_turn" || out.result.Evidence != "rpc_terminal" || !errors.Is(out.err, ErrDisconnected) ||
		errors.Is(out.err, ErrOutcomeUnknown) || errors.Is(out.err, ErrCancelUnconfirmed) || !errors.As(out.err, &failure) || !failure.Process.TreeStopped {
		t.Fatal("terminal-before-cancel lost its distinction/cleanup", out)
	}
	raw, _ := os.ReadFile(journal)
	if strings.Contains(string(raw), "session/cancel") {
		t.Fatal("terminal-before-send still emitted cancellation", string(raw))
	}
	p.mu.Lock()
	retired := p.prompt == nil
	p.mu.Unlock()
	if !retired {
		t.Fatal("cancel writer survived prompt return")
	}
}

func TestCancelReceivedTerminalSuppressesQueuedNotificationAndKeepsPeer(t *testing.T) {
	p, s, journal := cancelFixture(t, "terminal-before-send")
	signal := make(chan struct{})
	done := startCancelable(s, t.Context(), strings.Repeat("\"", 65536), signal)
	awaitCancelState(t, p, func(p *pendingPrompt) bool { return true })
	close(signal)
	awaitCancelState(t, p, func(p *pendingPrompt) bool { return p.cancelWriteDone != nil })
	continued := filepath.Join(filepath.Dir(journal), "continue")
	if err := os.WriteFile(continued, []byte("continue"), 0600); err != nil {
		t.Fatal(err)
	}
	awaitCancelState(t, p, func(p *pendingPrompt) bool { return p.ready })
	if err := os.WriteFile(continued+".drain", []byte("drain"), 0600); err != nil {
		t.Fatal(err)
	}
	out := awaitCancelable(t, done)
	if out.err != nil || out.result.StopReason != "end_turn" || p.Err() != nil {
		t.Fatal("already received terminal still emitted/retired", out, p.Err())
	}
	next, err := s.PromptText(t.Context(), "after-cancel")
	if err != nil || next.LocalRequestID != 4 || next.StopReason != "end_turn" {
		t.Fatal("suppressed cancel reached next prompt", next, err)
	}
	p.Close()
	raw, _ := os.ReadFile(journal)
	if strings.Contains(string(raw), "session/cancel") || strings.Count(string(raw), "session/prompt") != 2 {
		t.Fatal("queued cancel was not suppressed", string(raw))
	}
}

func TestCancelBlockedWriterKeepsSingleflightAndKnownTerminal(t *testing.T) {
	for _, reason := range []string{"cancelled", "end_turn"} {
		t.Run(reason, func(t *testing.T) {
			p, s, journal := cancelFixture(t, "blocked-writer-"+reason)
			ctx, stop := context.WithCancel(t.Context())
			defer stop()
			signal := make(chan struct{})
			done := startCancelable(s, ctx, "held", signal)
			awaitUpdates(t, s, 2)
			filler := make(chan error, 1)
			go func() { filler <- p.process.Write([]byte(strings.Repeat(" ", 1<<20))) }()
			awaitUpdates(t, s, 3) // Child read one filler byte, so filler owns Write's lock.
			close(signal)
			awaitCancelState(t, p, func(p *pendingPrompt) bool { return p.cancelAttempted })
			if err := os.WriteFile(filepath.Join(filepath.Dir(journal), "continue"), []byte("continue"), 0600); err != nil {
				t.Fatal(err)
			}
			awaitCancelState(t, p, func(p *pendingPrompt) bool { return p.ready })
			select {
			case out := <-done:
				t.Fatal("cancel writer was not joined before terminal return", out)
			default:
			}
			if _, err := s.PromptText(t.Context(), "must-not-follow"); !errors.Is(err, ErrPromptBusy) {
				t.Fatal("new prompt raced a stale cancel writer", err)
			}
			if _, err := s.SetConfigOption(t.Context(), "must-not-change", "value"); !errors.Is(err, ErrSessionBusy) {
				t.Fatal("config raced a stale cancel writer", err)
			}
			stop()
			out := awaitCancelable(t, done)
			select {
			case <-filler:
			case <-time.After(5 * time.Second):
				t.Fatal("owned filler writer not reaped")
			}
			var failure *PromptError
			if out.result.StopReason != reason || out.result.Evidence != "rpc_terminal" || !errors.Is(out.err, ErrDisconnected) ||
				errors.Is(out.err, ErrOutcomeUnknown) || !errors.As(out.err, &failure) || !failure.Process.TreeStopped {
				t.Fatal("valid original terminal lost during cancel-write failure", out)
			}
			if errors.Is(out.err, ErrCancelUnconfirmed) != (reason != "cancelled") {
				t.Fatal("native cancelled report was confused with another terminal", out)
			}
			p.mu.Lock()
			retired := p.prompt == nil
			p.mu.Unlock()
			if !retired {
				t.Fatal("writer survived invocation")
			}
		})
	}
}

func TestCancelConcurrentCloseAndCallerCancellationReapExactlyOwnedPeer(t *testing.T) {
	p, s, journal := cancelFixture(t, "no-terminal")
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	signal := make(chan struct{})
	done := startCancelable(s, ctx, "held", signal)
	awaitUpdates(t, s, 2)
	close(signal)
	awaitCancelFrame(t, journal)
	closed := make(chan process.Result, 4)
	for i := 0; i < cap(closed); i++ {
		go func() { status, _ := p.Close(); closed <- status }()
	}
	stop()
	out := awaitCancelable(t, done)
	var failure *PromptError
	if out.result.Evidence != "" || !errors.Is(out.err, ErrOutcomeUnknown) || !errors.As(out.err, &failure) || !failure.Process.TreeStopped {
		t.Fatal(out)
	}
	for i := 0; i < cap(closed); i++ {
		select {
		case status := <-closed:
			if !status.TreeStopped {
				t.Fatal(status)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent Close deadlocked")
		}
	}
	raw, _ := os.ReadFile(journal)
	if strings.Count(string(raw), "session/cancel") != 1 || strings.Count(string(raw), "session/prompt") != 1 {
		t.Fatal("close/retry race emitted native work", string(raw))
	}
}
