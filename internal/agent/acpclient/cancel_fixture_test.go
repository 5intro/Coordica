package acpclient

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// A hand-written simulation that keeps reading stdin while a prompt is active.
type cancelFixtureState struct{ pending, notifications int }

func (f *cancelFixtureState) prompt(scenario string, id int, text string) {
	if f.pending != 0 {
		os.Exit(15)
	}
	if scenario == "cancel-terminal-before-send" && id == 3 {
		return // Deliberate early response was emitted before stdin was drained.
	}
	if scenario == "cancel-natural" || text == "after-cancel" {
		cancelFixtureTerminal(id, "end_turn")
		return
	}
	f.pending = id
	for i := 0; i < 2; i++ {
		printUpdateFixture("native-session", map[string]any{
			"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "private-cancel-output"},
		})
	}
	if strings.HasPrefix(scenario, "cancel-blocked-writer-") {
		// Reading one filler byte proves that the parent's large Write owns the
		// real stdin writer. Stop draining it before the cancel notification.
		if _, err := io.ReadFull(os.Stdin, make([]byte, 1)); err != nil {
			os.Exit(16)
		}
		printUpdateFixture("native-session", map[string]any{
			"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "filler-observed"},
		})
		waitFixtureContinue()
		reason := strings.TrimPrefix(scenario, "cancel-blocked-writer-")
		cancelFixtureTerminal(id, reason)
		time.Sleep(30 * time.Second) // The parent must actually stop/reap this child.
		os.Exit(17)
	}
}

func waitCancelFixtureDrain() {
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(os.Getenv("COORDICA_ACP_CONTINUE") + ".drain"); err == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	os.Exit(20)
}

func (f *cancelFixtureState) cancel(scenario string, raw []byte) {
	var fields map[string]json.RawMessage
	var params map[string]string
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 3 || string(fields["jsonrpc"]) != `"2.0"` ||
		string(fields["method"]) != `"session/cancel"` || json.Unmarshal(fields["params"], &params) != nil ||
		len(params) != 1 || params["sessionId"] != "native-session" || f.pending == 0 || f.notifications != 0 {
		os.Exit(18)
	}
	if _, exists := fields["id"]; exists { // Null/zero IDs are not notifications either.
		os.Exit(19)
	}
	f.notifications++
	id := f.pending
	switch scenario {
	case "cancel-status":
		emitStatusFixture()
		waitFixtureContinue()
		cancelFixtureTerminal(id, "cancelled")
	case "cancel-no-terminal":
		return // Successful stdin delivery has no corresponding terminal/ACK.
	case "cancel-disconnect":
		os.Exit(0)
	case "cancel-wrong-id":
		cancelFixtureTerminal(id+100, "cancelled")
	case "cancel-ack-only":
		fmt.Println(`{"jsonrpc":"2.0","id":0,"result":{"cancelled":true}}`)
	case "cancel-other-terminal":
		cancelFixtureTerminal(id, "end_turn")
	case "cancel-rejected":
		raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{
			"code": -32000, "message": "private-cancel-rejection", "data": "private-cancel-data",
		}})
		fmt.Println(string(raw))
	default:
		printUpdateFixture("native-session", map[string]any{
			"sessionUpdate": "tool_call_update", "toolCallId": "native-tool", "status": "failed",
		})
		cancelFixtureTerminal(id, "cancelled")
	}
	f.pending = 0
}

func cancelFixtureTerminal(id int, reason string) {
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]string{"stopReason": reason}})
	fmt.Println(string(raw))
}
