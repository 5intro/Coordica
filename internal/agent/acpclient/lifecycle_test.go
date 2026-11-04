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

func TestInitializeRejectsInvalidOrUncertainWireAndWaitsForStop(t *testing.T) {
	for _, scenario := range []string{"wrong-id", "duplicate-fields", "both", "result-array", "bad-version", "null-caps", "null-auth", "null-bool", "callback", "stdout-noise", "oversize", "disconnect", "rejected", "duplicate-response"} {
		t.Run(scenario, func(t *testing.T) {
			config, _ := fixtureConfig(t, scenario)
			p, err := Open(t.Context(), config)
			if p != nil {
				defer p.Close()
				select {
				case <-p.Done():
				case <-time.After(3 * time.Second):
					t.Fatal("invalid duplicate response left Peer alive")
				}
				status, _ := p.Close()
				if scenario != "duplicate-response" || !status.TreeStopped || !errors.Is(p.Err(), ErrProtocol) {
					t.Fatal("invalid initialize returned a usable Peer", p.Err())
				}
				return
			}
			var failure *InitializationError
			if !errors.As(err, &failure) || !failure.Process.TreeStopped {
				t.Fatalf("missing actual cleanup proof: %v", err)
			}
			if strings.Contains(err.Error(), "private-") {
				t.Fatal("native private payload entered error text")
			}
			if scenario == "bad-version" {
				if !errors.Is(err, ErrUnsupportedVersion) || errors.Is(err, ErrOutcomeUnknown) {
					t.Fatal(err)
				}
			} else if scenario == "rejected" {
				var rpc *RPCError
				if !errors.As(err, &rpc) || rpc.Message != "private-acp-rejection" || errors.Is(err, ErrOutcomeUnknown) {
					t.Fatal("explicit rejection misclassified", err)
				}
			} else if !errors.Is(err, ErrOutcomeUnknown) {
				t.Fatal("uncertain initialize did not stay unknown", err)
			}
		})
	}
}

func TestInitializeOptionalDefaultsStderrAndPostHandshakeFailure(t *testing.T) {
	for _, scenario := range []string{"minimal", "stderr", "post-callback"} {
		t.Run(scenario, func(t *testing.T) {
			config, journal := fixtureConfig(t, scenario)
			p, err := Open(t.Context(), config)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			if scenario == "minimal" && (p.Info().AgentInfo != nil || p.Info().AgentCapabilities.LoadSession || len(p.Info().AuthMethods) != 0) {
				t.Fatal("omitted optional fields invented capabilities", p.Info())
			}
			if scenario == "post-callback" {
				if err := os.WriteFile(filepath.Join(filepath.Dir(journal), "continue"), []byte("continue"), 0600); err != nil {
					t.Fatal(err)
				}
				select {
				case <-p.Done():
				case <-time.After(3 * time.Second):
					t.Fatal("unsupported post-handshake notification not fenced")
				}
				if !errors.Is(p.Err(), ErrProtocol) || strings.Contains(p.Err().Error(), "private-") {
					t.Fatal(p.Err())
				}
			}
			if status, _ := p.Close(); !status.TreeStopped {
				t.Fatal("owned child did not stop")
			}
		})
	}
}

func TestInitializeCancellationAndHostLifetimeAreSeparate(t *testing.T) {
	config, journal := fixtureConfig(t, "normal")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if p, err := Open(ctx, config); p != nil || !errors.Is(err, context.Canceled) {
		t.Fatal(p, err)
	}
	if _, err := os.Stat(journal); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("pre-cancelled initialization sent input")
	}
	for _, scenario := range []string{"no-stdin", "no-response"} {
		t.Run(scenario, func(t *testing.T) {
			config, _ := fixtureConfig(t, scenario)
			op, cancel := context.WithTimeout(t.Context(), 60*time.Millisecond)
			defer cancel()
			_, err := OpenWithLifetime(t.Context(), op, config)
			var failure *InitializationError
			if !errors.Is(err, ErrOutcomeUnknown) || !errors.As(err, &failure) || !failure.Process.TreeStopped {
				t.Fatal("sent timeout lost uncertainty/stop", err)
			}
		})
	}
	host, cancelHost := context.WithCancel(t.Context())
	p, err := Open(host, config)
	if err != nil {
		cancelHost()
		t.Fatal(err)
	}
	cancelHost()
	select {
	case <-p.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("host cancellation did not stop owned child")
	}
	if status, _ := p.Close(); !status.TreeStopped {
		t.Fatal("host stopped without proof")
	}
}
