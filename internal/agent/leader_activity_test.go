package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLeaderActivitySupportsRegisteredProtocolsWithoutExposingPayloads(t *testing.T) {
	cases := []struct{ kind, payload, want string }{
		{"acp.message", `{"method":"session/update","params":{"update":{"sessionUpdate":"tool_call","rawInput":"PRIVATE"}}}`, "tool_started"},
		{"acp.message", `{"method":"session/update","params":{"update":{"sessionUpdate":"tool_call_update","status":"completed","content":"PRIVATE"}}}`, "tool_completed"},
		{"acp.message", `{"method":"session/update","params":{"update":{"sessionUpdate":"agent_thought_chunk","content":"PRIVATE"}}}`, ""},
		{"acp.message", `{"method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":"PRIVATE"}}}`, ""},
		{"assistant", `{"message":{"content":[{"type":"thinking","thinking":"PRIVATE"}]}}`, "result_received"},
		{"message_end", `{"message":{"role":"assistant","content":"PRIVATE"}}`, "result_received"},
		{"tool_execution_start", `{"args":"PRIVATE"}`, "tool_started"},
		{"reasoning", `{"text":"PRIVATE"}`, ""},
	}
	for _, tc := range cases {
		got, ok := LeaderPublicActivity(Event{Type: tc.kind, Data: json.RawMessage(tc.payload)})
		if ok != (tc.want != "") || ok && (got.Kind != tc.want || strings.Contains(got.Text+got.Key, "PRIVATE")) {
			t.Fatal("unsafe activity projection", tc.kind, got)
		}
	}
}
