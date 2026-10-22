package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCodexPublicActivityProjectsOnlyPublicFields(t *testing.T) {
	cases := []struct{ payload, kind, text string }{
		{`{"item":{"id":"one","type":"agent_message","text":"正在核对模块边界"}}`, "message", "正在核对模块边界"},
		{`{"item":{"id":"two","type":"reasoning","text":"PRIVATE_REASONING"}}`, "", ""},
		{`{"item":{"id":"three","type":"command_execution","command":"SECRET_COMMAND","aggregated_output":"SECRET_OUTPUT"}}`,
			"tool_completed", "工具操作已返回"},
		{`{"item":{"id":"four","type":"agent_message","text":"{\"reply\":\"PRIVATE_RESULT\"}"}}`,
			"result_received", "结构化结果已返回，等待中心核对"},
	}
	cases = append(cases, struct{ payload, kind, text string }{
		`{"item":{"id":"broken","type":"agent_message","text":"{malformed-private-json"}}`,
		"result_received", "结构化结果已返回，等待中心核对",
	})
	for _, test := range cases {
		result, ok := CodexPublicActivity(Event{Type: "item.completed", Data: json.RawMessage(test.payload)})
		if ok != (test.kind != "") || ok && (result.Kind != test.kind || result.Text != test.text) {
			t.Fatal(result, ok)
		}
		if strings.Contains(result.Text, "SECRET") || strings.Contains(result.Text, "PRIVATE") {
			t.Fatal("private protocol data exposed")
		}
	}
	a, _ := CodexPublicActivity(Event{Type: "turn.completed"})
	if a.Kind != "turn_finished" || !strings.Contains(a.Text, "等待中心") {
		t.Fatal("tool completion became business completion", a)
	}
}
