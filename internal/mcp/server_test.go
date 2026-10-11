package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type fakeCaller struct{ paths []string }

func (f *fakeCaller) Do(_ context.Context, method, path, key string, input any) (json.RawMessage, error) {
	f.paths = append(f.paths, method+" "+path)
	return json.RawMessage(`{"ok":true}`), nil
}
func TestRestrictedToolsAndScopeDoesNotGrant(t *testing.T) {
	f := &fakeCaller{}
	s := Server{API: f, RunID: "run_abc", Generation: 7}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"approve","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"request_scope","arguments":{"reason":"need src/x","idempotency_key":"scope1"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"get_context","arguments":{}}}`}, "\n")
	var out bytes.Buffer
	if e := s.Serve(context.Background(), strings.NewReader(input), &out); e != nil {
		t.Fatal(e)
	}
	if len(f.paths) != 2 || f.paths[0] != "POST /api/v1/runs/run_abc/block" {
		t.Fatalf("unexpected authority: %v", f.paths)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 {
		t.Fatal(out.String())
	}
	if !strings.Contains(lines[1], "Unknown tool") {
		t.Fatal(lines[1])
	}
}

func TestRunMCPRejectsMalformedCallsBeforeAPI(t *testing.T) {
	f := &fakeCaller{}
	s := Server{API: f, RunID: "run_abc", Generation: 7}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`,
		`{"jsonrpc":"2.0","id":null,"method":"tools/call","params":{"name":"get_context"}}`,
		`{"jsonrpc":"2.0","id":1.5,"method":"tools/call","params":{"name":"get_context"}}`,
		`{"jsonrpc":"2.0","id":[],"method":"tools/call","params":{"name":"get_context"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_context","arguments":null}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_context","arguments":[]}}`,
		`{"jsonrpc":"2.0","id":"valid","method":"tools/call","params":{"name":"get_context","_meta":{"progressToken":"progress"}}}`,
	}, "\n")
	var out bytes.Buffer
	if err := s.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	if len(f.paths) != 1 || f.paths[0] != "GET /api/v1/runs/run_abc/context" {
		t.Fatalf("invalid input reached API: %v", f.paths)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 7 {
		t.Fatal(out.String())
	}
	for _, line := range lines[1:4] {
		if !strings.Contains(line, `"code":-32600`) {
			t.Fatalf("expected invalid request: %s", line)
		}
	}
}
