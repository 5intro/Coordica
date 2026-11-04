package appserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Corrupt only a successful response body, after the fixture has applied its
// ordinary operation. These are valid JSON-RPC envelopes, not parse failures.
func fixtureResponseResult(scenario, method string, result any) any {
	switch scenario {
	case "typed-bad-result:" + method + ":array":
		return []any{}
	case "typed-bad-result:" + method + ":scalar":
		return "wrong result shape"
	default:
		return result
	}
}

func fixtureRecordMethod(method string) {
	path := os.Getenv("COORDICA_APP_SERVER_JOURNAL")
	if path == "" || method == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		os.Exit(8)
	}
	_, err = f.WriteString(method + "\n")
	if closeErr := f.Close(); err != nil || closeErr != nil {
		os.Exit(9)
	}
}

func responseFixture(t *testing.T, scenario string) (*Peer, string) {
	t.Helper()
	config := fixtureConfig(t, scenario)
	journal := filepath.Join(config.Directory, "methods.log")
	config.Env = append(config.Env, "COORDICA_APP_SERVER_JOURNAL="+journal)
	p, err := Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if status, err := p.Close(); !status.TreeStopped {
			t.Errorf("fixture process tree not stopped: %+v %v", status, err)
		}
	})
	return p, journal
}

func responseAction(t *testing.T, p *Peer, method string) func() error {
	t.Helper()
	thread, _, err := StartThread(t.Context(), p, fixtureOptions())
	if err != nil {
		t.Fatal(err)
	}
	switch method {
	case "thread/read":
		return func() error { _, err := thread.Read(t.Context(), true); return err }
	case "turn/start":
		return func() error { _, err := thread.StartText(t.Context(), "fixture work", "high"); return err }
	}
	turn, err := thread.StartText(t.Context(), "fixture work", "high")
	if err != nil {
		t.Fatal(err)
	}
	if frame := nextFrame(t, p); frame.Method != "turn/started" {
		t.Fatalf("missing initial turn: %s", frame.Method)
	}
	if method == "turn/steer" {
		return func() error { return turn.SteerText(t.Context(), "fixture supplement") }
	}
	return func() error { return turn.Interrupt(t.Context()) }
}

func pendingResponse(t *testing.T, p *Peer) <-chan error {
	t.Helper()
	finished := make(chan error, 1)
	go func() { finished <- p.Call(t.Context(), "fixture/pending", map[string]any{}, nil) }()
	if frame := nextFrame(t, p); frame.Method != "fixture/pending-registered" {
		t.Fatalf("pending call not registered: %s", frame.Method)
	}
	return finished
}

func TestMalformedSuccessfulResponsesStopPeer(t *testing.T) {
	for _, method := range []string{"turn/start", "turn/steer", "turn/interrupt", "thread/read"} {
		for _, shape := range []string{"array", "scalar"} {
			t.Run(method+"/"+shape, func(t *testing.T) {
				p, journal := responseFixture(t, "typed-bad-result:"+method+":"+shape)
				action := responseAction(t, p, method)
				pending := pendingResponse(t, p)
				if err := action(); !errors.Is(err, ErrProtocol) || !errors.Is(err, ErrOutcomeUnknown) {
					t.Errorf("malformed successful response lost uncertainty: %v", err)
				}
				if !errors.Is(p.Err(), ErrProtocol) {
					t.Errorf("malformed result did not fail connection: %v", p.Err())
				}
				assertClosedResponsePeer(t, p, journal, method)
				select {
				case err := <-pending:
					if !errors.Is(err, ErrOutcomeUnknown) {
						t.Errorf("pending call lost uncertainty: %v", err)
					}
				case <-time.After(time.Second):
					t.Error("pending call was not released by connection failure")
					p.Close()
					select {
					case <-pending:
					case <-time.After(time.Second):
						t.Fatal("pending call remained blocked after close")
					}
				}
				if status, err := p.Close(); !status.TreeStopped {
					t.Fatalf("malformed response process tree not stopped: %+v %v", status, err)
				}
			})
		}
	}
}

func assertClosedResponsePeer(t *testing.T, p *Peer, journal, method string) {
	t.Helper()
	before, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, line := range strings.Split(string(before), "\n") {
		if line == method {
			count++
		}
	}
	if count != 1 {
		t.Errorf("fixture operation count = %d, want 1", count)
	}
	if err := p.Call(t.Context(), "fixture/status", map[string]any{}, nil); !errors.Is(err, ErrDisconnected) {
		t.Errorf("closed peer accepted another call: %v", err)
	}
	after, err := os.ReadFile(journal)
	if err != nil || !bytes.Equal(before, after) {
		t.Errorf("closed peer sent another frame: %q %v", after, err)
	}
}

func TestNativeRejectionKeepsPeerUsable(t *testing.T) {
	p := openFixture(t, "typed")
	var result map[string]json.RawMessage
	err := p.Call(t.Context(), "fixture/explicit-rejection", map[string]any{}, &result)
	var rpc *RPCError
	if !errors.As(err, &rpc) || rpc.Code != -32601 || errors.Is(err, ErrOutcomeUnknown) || p.Err() != nil {
		t.Fatalf("native rejection changed classification or closed peer: %v %v", err, p.Err())
	}
	call(t, p, "fixture/status", map[string]any{}, &result)
}
