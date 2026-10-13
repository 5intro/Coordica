package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestChatMCPClosedToolsAndMalformedCallsNeverReachAPI(t *testing.T) {
	f := &fakeCaller{}
	s := Chat{API: f}
	ctx := context.Background()
	invalid := []struct{ name, raw string }{
		{"list_project_items", `{"kind":"unknown","limit":1}`}, {"list_project_items", `{"kind":"tasks","limit":21}`}, {"list_project_items", `{"kind":"tasks","limit":1,"cursor":"../../owner.token"}`}, {"list_project_items", `{"kind":"tasks","kind":"requirements","limit":1}`},
		{"get_project_overview", `{"url":"https://evil"}`}, {"get_project_overview", `null`}, {"get_project_overview", `[]`}, {"get_project_item", `{"kind":"tasks","id":"/etc/password"}`},
		{"consult_leader", `{"original_text":"Question","source":{"session_id":"s","message_id":"m"},"idempotency_key":"k"}`}, {"consult_leader", `{"expected_goal_version":0,"original_text":"Question","source":{"session_id":"s","session_id":"s2","message_id":"m"},"idempotency_key":"k"}`},
		{"submit_requirement", `{"title":"T","original_text":"Text","source":{"session_id":"s","message_id":"m"},"idempotency_key":"k","command":"cat owner.token"}`}, {"approve", `{}`}, {"initialize_runner", `{}`}, {"get_context", `{}`}, {"cancel_consultation", `{"id":"x","expected_version":0,"idempotency_key":"k"}`},
	}
	for _, v := range invalid {
		if _, e := s.call(ctx, v.name, []byte(v.raw)); e == nil {
			t.Error("malformed call accepted", v)
		}
	}
	if len(f.paths) != 0 {
		t.Fatal("invalid calls reached API", f.paths)
	}
	if _, e := s.call(ctx, "get_project_overview", []byte(`{}`)); e != nil {
		t.Fatal(e)
	}
	if _, e := s.call(ctx, "consult_leader", []byte(`{"expected_goal_version":0,"original_text":"User asks advice","assistant_summary":"Summary","source":{"session_id":"s","message_id":"m"},"idempotency_key":"stable"}`)); e != nil {
		t.Fatal(e)
	}
	if len(f.paths) != 2 || f.paths[0] != "GET /api/v1/chat/overview" || f.paths[1] != "POST /api/v1/chat/consultations" {
		t.Fatal(f.paths)
	}
	raw, _ := json.Marshal(chatTools())
	for _, name := range []string{"get_project_overview", "list_project_items", "get_project_item", "submit_requirement", "consult_leader", "propose_change", "get_consultation", "cancel_consultation"} {
		if !strings.Contains(string(raw), `"name":"`+name+`"`) {
			t.Fatal("tool missing", name)
		}
	}
	if len(chatTools()) != 11 {
		t.Fatal("unexpected tools")
	}
}

type failingChatAPI struct{}

func (failingChatAPI) Do(context.Context, string, string, string, any) (json.RawMessage, error) {
	return nil, errors.New("Bearer chat_credential /Users/private/owner.token raw command")
}
func TestChatMCPHandshakeAndSafeErrors(t *testing.T) {
	var out bytes.Buffer
	input := strings.Join([]string{`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"client","version":"1"}}}`, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_project_overview","arguments":{}}}`, `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`}, "\n")
	if e := (Chat{API: failingChatAPI{}}).Serve(context.Background(), strings.NewReader(input), &out); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(out.String(), "chat_credential") || strings.Contains(out.String(), "/Users/private") || !strings.Contains(out.String(), `"name":"coordica"`) {
		t.Fatal(out.String())
	}
	if len(strings.Split(strings.TrimSpace(out.String()), "\n")) != 3 {
		t.Fatal("notification response")
	}
}

type scopedChatAPI struct{ scope string }

func (f scopedChatAPI) Do(_ context.Context, _ string, path, _ string, _ any) (json.RawMessage, error) {
	if path == "/api/v1/chat/identity" {
		return json.Marshal(map[string]any{"chat_scope": f.scope, "revoked": false})
	}
	return []byte(`{}`), nil
}
func TestChatToolDiscoveryUsesAuthenticatedServerScope(t *testing.T) {
	for _, scope := range []string{"center", "node", ""} {
		t.Run(scope, func(t *testing.T) {
			input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{}}}` + "\n" + `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
			var out bytes.Buffer
			if e := (Chat{API: scopedChatAPI{scope: scope}}).Serve(context.Background(), strings.NewReader(input), &out); e != nil {
				t.Fatal(e)
			}
			lines := strings.Split(strings.TrimSpace(out.String()), "\n")
			var response map[string]json.RawMessage
			if e := json.Unmarshal([]byte(lines[1]), &response); e != nil {
				t.Fatal(e)
			}
			if scope == "" {
				if response["error"] == nil {
					t.Fatal("legacy unscoped discovery inferred center")
				}
				return
			}
			var result struct {
				Tools []struct {
					Name  string          `json:"name"`
					Input json.RawMessage `json:"inputSchema"`
				} `json:"tools"`
			}
			if e := json.Unmarshal(response["result"], &result); e != nil {
				t.Fatal(e)
			}
			want := 11
			if scope == "node" {
				want = 4
			}
			if len(result.Tools) != want {
				t.Fatal(scope, len(result.Tools))
			}
			if scope == "node" {
				for _, tool := range result.Tools {
					if strings.Contains(tool.Name, "consult") || tool.Name == "propose_change" || strings.Contains(string(tool.Input), "consultations") {
						t.Fatal("node tools imply Leader access", tool.Name)
					}
				}
			}
		})
	}
}
