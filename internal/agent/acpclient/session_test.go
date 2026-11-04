package acpclient

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewSessionPinsPeerAndHostDirectoryWithoutPromptOrLoad(t *testing.T) {
	config, journal := fixtureConfig(t, "session")
	p, err := Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	op, cancel := context.WithCancel(t.Context())
	s, err := p.NewSession(op)
	cancel()
	directory, _ := filepath.EvalSymlinks(config.Directory)
	if err != nil || s.ID() != "native-session" || s.Directory() != directory || s.peer != p || p.Err() != nil {
		t.Fatal(s, err, p.Err())
	}
	if _, err := p.NewSession(t.Context()); !errors.Is(err, ErrSessionAllocated) {
		t.Fatal("duplicate allocated a Session", err)
	}
	if _, err := p.LoadSession(t.Context(), s.ID()); !errors.Is(err, ErrUnsupported) {
		t.Fatal("advertised load became supported", err)
	}
	if status, _ := p.Close(); !status.TreeStopped {
		t.Fatal("local owned process did not stop")
	}
	raw, err := os.ReadFile(journal)
	if err != nil || strings.Count(string(raw), "\n") != 2 || strings.Contains(string(raw), "session/prompt") || strings.Contains(string(raw), "session/load") || strings.Contains(string(raw), "session/close") || strings.Contains(string(raw), "authenticate") {
		t.Fatalf("extra native operation: %s %v", raw, err)
	}
}

func TestNewSessionKnownRejectionAndUncertainAllocationAreDistinct(t *testing.T) {
	for _, scenario := range []string{"rejected", "wrong-id", "null-result", "empty-id", "long-id", "bad-options", "bad-modes", "callback", "disconnect", "duplicate-response"} {
		t.Run(scenario, func(t *testing.T) {
			config, journal := fixtureConfig(t, "session-"+scenario)
			p, err := Open(t.Context(), config)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			s, err := p.NewSession(t.Context())
			if scenario == "rejected" {
				var rpc *RPCError
				if !errors.As(err, &rpc) || errors.Is(err, ErrOutcomeUnknown) || p.Err() != nil || strings.Contains(err.Error(), "private-") || s.ID() != "" {
					t.Fatal("explicit rejection became unknown or leaked", err)
				}
				if _, err := p.NewSession(t.Context()); !errors.Is(err, ErrSessionAllocated) {
					t.Fatal("known rejection retried", err)
				}
			} else if err == nil {
				if scenario != "duplicate-response" {
					t.Fatal("invalid response allocated handle", s)
				}
				select {
				case <-p.Done():
				case <-time.After(3 * time.Second):
					t.Fatal("duplicate response was not fenced")
				}
				if status, _ := p.Close(); !status.TreeStopped || !errors.Is(p.Err(), ErrProtocol) {
					t.Fatal(p.Err())
				}
			} else {
				var failure *SessionCreationError
				if s.ID() != "" || !errors.Is(err, ErrOutcomeUnknown) || !errors.As(err, &failure) || !failure.Process.TreeStopped || strings.Contains(err.Error(), "private-") {
					t.Fatal("uncertain allocation lost fence/proof", err)
				}
				if (scenario == "bad-options" || scenario == "bad-modes") && failure.NativeSessionID != "native-session" {
					t.Fatal("possible allocated native ID observation lost", failure)
				}
			}
			raw, err := os.ReadFile(journal)
			if err != nil || strings.Count(string(raw), "session/new") != 1 || strings.Contains(string(raw), "authenticate") || strings.Contains(string(raw), "session/close") {
				t.Fatalf("unexpected fallback/retry: %s %v", raw, err)
			}
		})
	}
}

func awaitSessionRequest(t *testing.T, journal string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		raw, _ := os.ReadFile(journal)
		if strings.Contains(string(raw), "session/new") {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("session/new was not observed")
}

func TestNewSessionPreCancelAndConcurrentCallsDoNotConsumeAnotherAllocation(t *testing.T) {
	config, journal := fixtureConfig(t, "session-pause")
	p, err := Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = p.NewSession(ctx); !errors.Is(err, context.Canceled) || p.Err() != nil {
		t.Fatal("pre-cancel damaged Peer", err)
	}
	done := make(chan error, 1)
	go func() { _, err := p.NewSession(t.Context()); done <- err }()
	awaitSessionRequest(t, journal)
	if _, err = p.NewSession(t.Context()); !errors.Is(err, ErrSessionAllocated) || p.Err() != nil {
		t.Fatal("concurrent allocation duplicated/stopped Peer", err)
	}
	if err = os.WriteFile(filepath.Join(filepath.Dir(journal), "continue"), []byte("continue"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(journal)
	if strings.Count(string(raw), "session/new") != 1 {
		t.Fatal("concurrent request allocated twice")
	}
}

func TestNewSessionSentCancellationAndTimeoutWaitForRealTreeStop(t *testing.T) {
	for _, scenario := range []string{"cancel", "timeout"} {
		t.Run(scenario, func(t *testing.T) {
			config, journal := fixtureConfig(t, "session-no-response")
			p, err := Open(t.Context(), config)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			var ctx context.Context
			var cancel context.CancelFunc
			if scenario == "timeout" {
				ctx, cancel = context.WithTimeout(t.Context(), 60*time.Millisecond)
			} else {
				ctx, cancel = context.WithCancel(t.Context())
			}
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := p.NewSession(ctx); done <- err }()
			awaitSessionRequest(t, journal)
			if scenario == "cancel" {
				cancel()
			}
			err = <-done
			var failure *SessionCreationError
			if !errors.Is(err, ErrOutcomeUnknown) || !errors.As(err, &failure) || !failure.Process.TreeStopped || failure.NativeSessionID != "" {
				t.Fatal("sent uncertain allocation did not stop/prove unknown", err)
			}
			if _, err = p.NewSession(t.Context()); err == nil {
				t.Fatal("failed Peer allocated again")
			}
		})
	}
}
