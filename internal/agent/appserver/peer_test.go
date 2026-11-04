package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixtureConfig(t *testing.T, scenario string) Config {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fixture.txt"), []byte("offline fixture content"), 0600); err != nil {
		t.Fatal(err)
	}
	return Config{Executable: executable, Directory: dir, Experimental: true,
		Env: append(os.Environ(), "COORDICA_APP_SERVER_FIXTURE=1", "COORDICA_APP_SERVER_SCENARIO="+scenario,
			"GORACE=atexit_sleep_ms=0")}
}

func openFixture(t *testing.T, scenario string) *Peer {
	t.Helper()
	peer, err := Open(t.Context(), fixtureConfig(t, scenario))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		status, err := peer.Close()
		if !status.TreeStopped {
			t.Errorf("fixture tree was not stopped: %+v %v", status, err)
		}
	})
	return peer
}

func call(t *testing.T, p *Peer, method string, params, result any) {
	t.Helper()
	if err := p.Call(t.Context(), method, params, result); err != nil {
		t.Fatalf("%s: %v", method, err)
	}
}

func nextFrame(t *testing.T, p *Peer) Frame {
	t.Helper()
	select {
	case frame, ok := <-p.Events():
		if !ok {
			t.Fatalf("connection ended: %v", p.Err())
		}
		return frame
	case <-time.After(3 * time.Second):
		t.Fatal("missing fixture frame")
		return Frame{}
	}
}

func TestTwoTurnsShareConnectionAndResumeDoesNotReplay(t *testing.T) {
	p := openFixture(t, "")
	if p.Info().UserAgent != "codex-fixture/0.163.0" || p.Info().PlatformOS != "fixture" {
		t.Fatalf("initialization facts lost: %+v", p.Info())
	}
	var start struct {
		Thread struct{ ID string } `json:"thread"`
	}
	call(t, p, "thread/start", map[string]string{"cwd": "fixture-directory"}, &start)
	if start.Thread.ID != "native-thread" {
		t.Fatal(start)
	}
	for i := 1; i <= 2; i++ {
		call(t, p, "turn/start", map[string]any{"threadId": start.Thread.ID,
			"input": []map[string]string{{"type": "text", "text": fmt.Sprintf("round %d", i)}}}, nil)
		for _, method := range []string{"turn/started", "item/agentMessage/delta", "turn/completed"} {
			frame := nextFrame(t, p)
			if frame.Method != method {
				t.Fatalf("got %s, want %s", frame.Method, method)
			}
			if method == "item/agentMessage/delta" && !strings.Contains(string(frame.Params), "offline fixture content") {
				t.Fatalf("fixture output missing: %s", frame.Params)
			}
		}
	}
	var status struct {
		Turns int `json:"turns"`
	}
	call(t, p, "thread/resume", map[string]string{"threadId": start.Thread.ID}, &status)
	call(t, p, "thread/read", map[string]string{"threadId": start.Thread.ID}, &status)
	if status.Turns != 2 {
		t.Fatalf("resume started work: %+v", status)
	}
}

func TestSteerAndInterruptPreserveNativeTargetWithoutStartingNextTurn(t *testing.T) {
	p := openFixture(t, "control")
	call(t, p, "thread/start", map[string]any{}, nil)
	call(t, p, "turn/start", map[string]string{"threadId": "native-thread"}, nil)
	nextFrame(t, p)
	steer := map[string]any{"threadId": "native-thread", "expectedTurnId": "old-turn",
		"input": []map[string]string{{"type": "text", "text": "additional fixture context"}}}
	err := p.Call(t.Context(), "turn/steer", steer, nil)
	var rejection *RPCError
	if !errors.As(err, &rejection) || rejection.Code != -32001 || strings.Contains(err.Error(), "fixture-private-detail") {
		t.Fatalf("wrong native rejection: %v", err)
	}
	steer["expectedTurnId"] = "turn-1"
	call(t, p, "turn/steer", steer, nil)
	call(t, p, "turn/interrupt", map[string]string{"threadId": "native-thread", "turnId": "turn-1"}, nil)
	frame := nextFrame(t, p)
	if frame.Method != "turn/completed" || !strings.Contains(string(frame.Params), "interrupted") {
		t.Fatalf("missing interruption fact: %+v", frame)
	}
	var status struct{ Turns int }
	call(t, p, "fixture/status", map[string]any{}, &status)
	if status.Turns != 1 {
		t.Fatalf("interrupt created another turn: %+v", status)
	}
}

func TestNativeRequestsAreConnectionBoundAndRepliesOnlyQueued(t *testing.T) {
	p, other := openFixture(t, ""), openFixture(t, "")
	for _, peer := range []*Peer{p, other} {
		call(t, peer, "fixture/requests", map[string]any{}, nil)
	}
	question, approval := nextFrame(t, p), nextFrame(t, p)
	if string(question.ID) != "7" || string(approval.ID) != `"7"` {
		t.Fatalf("numeric/string identities conflated: %s %s", question.ID, approval.ID)
	}
	if _, err := other.Respond(question, map[string]any{}); !errors.Is(err, ErrStaleRequest) {
		t.Fatalf("old connection callback accepted: %v", err)
	}
	receipt, err := p.Respond(question, map[string]any{"answers": map[string]any{}})
	if err != nil || receipt.Disposition != "queued" {
		t.Fatalf("incorrect delivery claim: %+v %v", receipt, err)
	}
	if _, err := p.Respond(question, map[string]any{}); !errors.Is(err, ErrStaleRequest) {
		t.Fatalf("duplicate answer accepted: %v", err)
	}
	if _, err := p.Reject(approval, -32601, "unsupported by the caller"); err != nil {
		t.Fatal(err)
	}
	var status struct{ Replies int }
	call(t, p, "fixture/status", map[string]any{}, &status)
	if status.Replies != 2 {
		t.Fatalf("wrong reply routing: %+v", status)
	}
	p.Close()
	if _, err := p.Respond(question, map[string]any{}); !errors.Is(err, ErrStaleRequest) {
		t.Fatalf("closed callback accepted: %v", err)
	}
}

func TestConcurrentCallsMatchOutOfOrderResponses(t *testing.T) {
	p := openFixture(t, "")
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var result struct{ Value int }
			err := p.Call(t.Context(), "fixture/reorder", map[string]int{"value": i}, &result)
			if err != nil || result.Value != i {
				t.Errorf("response crossed calls: %d %+v %v", i, result, err)
			}
		}(i)
	}
	wg.Wait()
}

func TestProtocolFailuresAndDisconnectDoNotBecomeSuccess(t *testing.T) {
	for _, scenario := range []string{"malformed", "bad-id", "disconnect"} {
		t.Run(scenario, func(t *testing.T) {
			p := openFixture(t, "")
			err := p.Call(t.Context(), "fixture/"+scenario, map[string]any{}, nil)
			if !errors.Is(err, ErrOutcomeUnknown) {
				t.Fatalf("missing uncertainty: %v", err)
			}
			if scenario != "disconnect" && !errors.Is(p.Err(), ErrProtocol) {
				t.Fatalf("wrong protocol failure: %v", p.Err())
			}
		})
	}
}

func TestCallTimeoutStopsConnectionWithoutReplay(t *testing.T) {
	p := openFixture(t, "")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	err := p.Call(ctx, "fixture/hang", map[string]any{}, nil)
	if !errors.Is(err, ErrOutcomeUnknown) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout became a definite rejection: %v", err)
	}
	status, _ := p.Close()
	if !status.TreeStopped {
		t.Fatal("timed-out fixture still runs")
	}
}

func TestCancelledBeforeQueueLeavesConnectionUsable(t *testing.T) {
	p := openFixture(t, "")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := p.Call(ctx, "fixture/hang", map[string]any{}, nil)
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrOutcomeUnknown) {
		t.Fatalf("unsent call reported uncertain: %v", err)
	}
	call(t, p, "fixture/status", map[string]any{}, nil)
}

func TestSlowConsumerFailsExplicitlyAndOutboundLimitDoesNotSend(t *testing.T) {
	p := openFixture(t, "")
	err := p.Call(t.Context(), "fixture/hang", map[string]string{"text": strings.Repeat("x", maxFrameBytes)}, nil)
	if !errors.Is(err, ErrBackpressure) {
		t.Fatalf("oversized request accepted: %v", err)
	}
	call(t, p, "fixture/status", map[string]any{}, nil)
	p.Call(t.Context(), "fixture/flood", map[string]any{}, nil)
	select {
	case <-p.failed:
		if !errors.Is(p.Err(), ErrBackpressure) {
			t.Fatalf("wrong slow-consumer failure: %v", p.Err())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("slow consumer blocked protocol shutdown")
	}
}

func TestDuplicateRequestAndChangedCallbackCannotBroadenAnswer(t *testing.T) {
	p := openFixture(t, "")
	call(t, p, "fixture/duplicate-request", map[string]any{}, nil)
	request := nextFrame(t, p)
	changed := request
	changed.Params = json.RawMessage(`{"threadId":"other"}`)
	if _, err := p.Respond(changed, map[string]any{}); !errors.Is(err, ErrStaleRequest) {
		t.Fatalf("changed callback accepted: %v", err)
	}
	if _, err := p.Respond(request, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	select {
	case frame := <-p.Events():
		t.Fatalf("duplicate request delivered: %+v", frame)
	default:
	}
}

func TestOpenRejectsImplicitEnvironmentAndInvalidHandshake(t *testing.T) {
	config := fixtureConfig(t, "")
	config.Env = nil
	if _, err := Open(t.Context(), config); err == nil {
		t.Fatal("implicitly inherited execution authority")
	}
	if _, err := Open(t.Context(), fixtureConfig(t, "bad-init")); !errors.Is(err, ErrProtocol) {
		t.Fatalf("invalid initialization accepted: %v", err)
	}
}

func TestExperimentalCapabilityIsExplicit(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			config := fixtureConfig(t, "")
			config.Experimental = enabled
			p, err := Open(t.Context(), config)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if status, err := p.Close(); !status.TreeStopped {
					t.Errorf("fixture tree was not stopped: %+v %v", status, err)
				}
			})
			var status struct{ Experimental bool }
			call(t, p, "fixture/status", map[string]any{}, &status)
			if status.Experimental != enabled {
				t.Fatalf("capability silently changed: %+v", status)
			}
		})
	}
}

func TestMalformedResponseCannotLookLikeNativeRejection(t *testing.T) {
	for _, raw := range []string{
		`{"id":"x","error":{}}`,
		`{"id":"x","error":{"code":null,"message":"invalid"}}`,
		`{"id":"x","result":{},"error":null}`,
		`{"id":"x","method":"","result":{}}`,
		`{"id":"x","jsonrpc":null,"result":{}}`,
	} {
		p := &Peer{waiters: map[string]chan response{"s:x": make(chan response, 1)}}
		if err := p.receive([]byte(raw)); !errors.Is(err, ErrProtocol) {
			t.Fatalf("malformed response accepted: %s %v", raw, err)
		}
	}
	p := &Peer{}
	if err := p.receive([]byte("{\"method\":\"invalid\xff\"}")); !errors.Is(err, ErrProtocol) {
		t.Fatalf("invalid UTF-8 accepted: %v", err)
	}
}

func TestOversizedNativeFrameStopsSupervisedProcess(t *testing.T) {
	p := openFixture(t, "")
	err := p.Call(t.Context(), "fixture/oversized", map[string]any{}, nil)
	if !errors.Is(err, ErrOutcomeUnknown) {
		t.Fatalf("oversized native frame became success: %v", err)
	}
	if status, err := p.Close(); !status.TreeStopped {
		t.Fatalf("oversized peer remained alive: %+v %v", status, err)
	}
}
