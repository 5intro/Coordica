package mcp

import (
	"context"
	"coordica/internal/domain"
	"testing"
)

func TestLeaderActivityMCPBoundedAndCenterOnly(t *testing.T) {
	caller := &fakeCaller{}
	chat := Chat{API: caller}
	for _, raw := range []string{`{}`, `{"limit":101}`, `{"limit":1,"url":"http://evil"}`, `{"limit":1,"generation":1}`} {
		if _, err := chat.call(context.Background(), "read_leader_activity", []byte(raw)); err == nil {
			t.Fatal("invalid query accepted", raw)
		}
	}
	if len(caller.paths) != 0 {
		t.Fatal("malformed call reached transport")
	}
	if _, err := chat.call(context.Background(), "read_leader_activity", []byte(`{"limit":10,"after":0}`)); err != nil {
		t.Fatal(err)
	}
	if caller.paths[0] != "POST /api/v1/chat/leader/activity" {
		t.Fatal(caller.paths)
	}
	if hasTool(chatToolsForScope(domain.ChatScopeNode), "read_leader_activity") {
		t.Fatal("node gained Leader tool")
	}
	if !hasTool(chatToolsForScope(domain.ChatScopeCenter), "read_leader_activity") {
		t.Fatal("center lacks tool")
	}
	if _, err := chat.call(context.Background(), "open_project", []byte(`{"kind":"existing","expected_version":0,"idempotency_key":""}`)); err == nil {
		t.Fatal("extracted preparation route lost key validation")
	}
}
