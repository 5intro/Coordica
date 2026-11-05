package acpclient

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type permissionRaceFixtureState struct {
	prompt   int
	selected bool
}

func (f *permissionRaceFixtureState) handle(scenario string, raw []byte) bool {
	var frame struct {
		Method string
		ID     json.RawMessage
		Params struct {
			SessionID string
			Prompt    []struct{ Type, Text string }
		}
		Result struct {
			Outcome struct{ Outcome, OptionID string }
		}
	}
	if json.Unmarshal(raw, &frame) != nil {
		os.Exit(61)
	}
	switch frame.Method {
	case "session/prompt":
		if frame.Params.SessionID != "native-session" || len(frame.Params.Prompt) != 1 {
			os.Exit(62)
		}
		if frame.Params.Prompt[0].Text == "after-permission" {
			var id int
			json.Unmarshal(frame.ID, &id)
			cancelFixtureTerminal(id, "end_turn")
			return true
		}
		if json.Unmarshal(frame.ID, &f.prompt) != nil || f.prompt != 3 {
			os.Exit(63)
		}
		permissionRaceCallback()
		// The parent starts its large filler only after observing the callback.
		// One direct byte proves the real stdin Write lock is now held by it.
		if _, err := io.ReadFull(os.Stdin, make([]byte, 1)); err != nil {
			os.Exit(64)
		}
		printUpdateFixture("native-session", map[string]any{
			"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "writer-owned"},
		})
		waitFixtureContinue()
		if scenario == "permission-race-terminal-block" {
			cancelFixtureTerminal(f.prompt, "end_turn")
			time.Sleep(30 * time.Second)
			os.Exit(65)
		}
		if _, err := io.CopyN(io.Discard, os.Stdin, (1<<20)-1); err != nil {
			os.Exit(66)
		}
		return true
	case "session/cancel":
		if scenario != "permission-race-cancel-order" || !f.selected || frame.Params.SessionID != "native-session" || frame.ID != nil {
			os.Exit(67)
		}
		cancelFixtureTerminal(f.prompt, "cancelled")
		return true
	case "":
		if strings.TrimSpace(string(frame.ID)) != "41" || frame.Result.Outcome.Outcome != "selected" || frame.Result.Outcome.OptionID != "allow" || f.selected {
			os.Exit(68)
		}
		f.selected = true
		return true
	}
	return false
}

func permissionRaceCallback() {
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 41, "method": "session/request_permission", "params": map[string]any{
		"sessionId": "native-session", "toolCall": map[string]any{"toolCallId": "private-race-tool", "title": "private-race-title"},
		"options": []any{map[string]any{"optionId": "allow", "name": "private-allow", "kind": "allow_once"},
			map[string]any{"optionId": "reject", "name": "private-reject", "kind": "reject_once"}},
	}})
	fmt.Println(string(raw))
}
