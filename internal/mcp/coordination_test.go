package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"coordica/internal/domain"
)

func TestWorkMCPClosedProtocolAndPinnedWrite(t *testing.T) {
	f := &fakeCaller{}
	s := Chat{API: f}
	ctx := context.Background()
	for _, test := range []struct{ name, body string }{
		{"list_work_contexts", `{"limit":0}`},
		{"get_work_context", `{"anchor":{"kind":"task","id":"/etc/passwd"},"limit":1}`},
		{"get_work_context", `{"anchor":{"kind":"task","id":"task_1"},"limit":1,"command":"run"}`},
		{"record_work_note", `{"anchor":{"kind":"task","id":"task_1"},"kind":"approve","original_text":"approve"}`},
	} {
		if _, e := s.call(ctx, test.name, []byte(test.body)); e == nil {
			t.Fatal("invalid call accepted", test)
		}
	}
	if len(f.paths) != 0 {
		t.Fatal("invalid calls reached API")
	}
	if _, e := s.call(ctx, "get_work_context", []byte(`{"anchor":{"kind":"task","id":"task_1"},"limit":1}`)); e != nil {
		t.Fatal(e)
	}
	in := struct {
		domain.WorkNoteInput
		Key string `json:"idempotency_key"`
	}{domain.WorkNoteInput{Anchor: domain.WorkAnchor{Kind: "task", ID: "task_1"}, ExpectedStamp: strings.Repeat("a", 64), Kind: "note", OriginalText: "Original", References: []domain.WorkReference{}, Source: domain.ChatSource{SessionID: "s", MessageID: "m"}}, "stable"}
	b, _ := json.Marshal(in)
	if _, e := s.call(ctx, "record_work_note", b); e != nil {
		t.Fatal(e)
	}
	if len(f.paths) != 2 || f.paths[0] != "POST /api/v1/chat/work/context" || f.paths[1] != "POST /api/v1/chat/work/notes" {
		t.Fatal(f.paths)
	}
	for _, scope := range []string{domain.ChatScopeCenter, domain.ChatScopeNode} {
		tools := chatToolsForScope(scope)
		for _, name := range []string{"list_work_contexts", "get_work_context", "record_work_note"} {
			if !hasTool(tools, name) {
				t.Fatal(scope, name)
			}
		}
	}
}
