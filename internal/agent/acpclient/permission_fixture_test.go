package acpclient

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// permissionFixtureState is an owned Go protocol child, never a real harness.
// It emits permission requests and validates replies without executing tools.
type permissionFixtureState struct {
	promptID  json.RawMessage
	turn      int
	responses int
	cancelled bool
	pending   map[string]map[string]bool
}

func (s *permissionFixtureState) handle(scenario string, raw []byte) bool {
	switch scenario {
	case "permission-normal", "permission-ids", "permission-empty", "permission-cancel-late",
		"permission-wrong-session", "permission-malformed", "permission-duplicate", "permission-pending-terminal",
		"permission-flood", "permission-large", "permission-budget", "permission-lifetime", "permission-reuse", "permission-opaque-option":
	default:
		return false
	}
	var envelope struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
		Result  json.RawMessage `json:"result"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		os.Exit(30)
	}
	switch envelope.Method {
	case "session/prompt":
		var params struct {
			SessionID string `json:"sessionId"`
			Prompt    []struct{ Type, Text string }
		}
		var id int64
		if envelope.JSONRPC != "2.0" || json.Unmarshal(envelope.ID, &id) != nil || id < 3 ||
			json.Unmarshal(envelope.Params, &params) != nil || params.SessionID != "native-session" ||
			len(params.Prompt) != 1 || params.Prompt[0].Type != "text" || params.Prompt[0].Text == "" {
			os.Exit(31)
		}
		s.promptID = append(json.RawMessage(nil), envelope.ID...)
		s.turn++
		s.responses, s.cancelled = 0, false
		s.pending = map[string]map[string]bool{}
		s.start(scenario)
		return true
	case "session/cancel":
		var params struct{ SessionID string }
		if envelope.JSONRPC != "2.0" || envelope.ID != nil || json.Unmarshal(envelope.Params, &params) != nil ||
			params.SessionID != "native-session" || s.promptID == nil || s.cancelled {
			os.Exit(32)
		}
		s.cancelled = true
		if scenario == "permission-cancel-late" {
			s.emit(json.RawMessage(`"late-after-cancel"`), "native-session", false, 0)
		}
		if len(s.pending) == 0 {
			s.terminal("cancelled")
		}
		return true
	case "":
		if envelope.Result == nil {
			return false
		}
		s.reply(scenario, raw, envelope.ID, envelope.Result)
		return true
	default:
		return false // initialize/new and unrelated fixture routes stay unchanged.
	}
}

func (s *permissionFixtureState) start(scenario string) {
	defaultID := json.RawMessage(fmt.Sprintf(`"permission-%d"`, s.turn))
	switch scenario {
	case "permission-opaque-option":
		s.pending["s:opaque-id"] = map[string]bool{"\ufffd": true}
		fmt.Println(`{"jsonrpc":"2.0","id":"opaque-id","method":"session/request_permission","params":` +
			`{"sessionId":"native-session","toolCall":{"toolCallId":"t"},"options":[` +
			`{"optionId":"\ud800","name":"private-opaque-choice","kind":"reject_once"}]}}`)
	case "permission-ids":
		for _, id := range []string{`7`, `"7"`, `null`, `-7`} {
			s.emit(json.RawMessage(id), "native-session", false, 0)
		}
	case "permission-empty":
		s.emit(defaultID, "native-session", true, 0)
	case "permission-cancel-late":
		s.emit(json.RawMessage(`"cancel-first"`), "native-session", false, 0)
		s.emit(json.RawMessage(`"cancel-second"`), "native-session", false, 0)
	case "permission-wrong-session":
		s.emit(defaultID, "other-session", false, 0)
	case "permission-malformed":
		fmt.Println(`{"jsonrpc":"2.0","id":41,"method":"session/request_permission",` +
			`"params":{"sessionId":"native-session","toolCall":{"toolCallId":"t"},"options":null}}`)
	case "permission-duplicate":
		s.emit(json.RawMessage(`0`), "native-session", true, 0)
		s.emit(json.RawMessage(`-0`), "native-session", true, 0)
	case "permission-pending-terminal":
		s.emit(defaultID, "native-session", false, 0)
		s.terminal("end_turn")
	case "permission-flood":
		for i := 0; i < 65; i++ {
			s.emit(json.RawMessage(fmt.Sprintf(`"flood-%d"`, i)), "native-session", true, 0)
		}
	case "permission-large":
		s.emit(defaultID, "native-session", false, 64<<10)
	case "permission-budget":
		s.emit(json.RawMessage(`"budget-a"`), "native-session", false, 40000)
		s.emit(json.RawMessage(`"budget-b"`), "native-session", false, 40000)
	case "permission-lifetime":
		s.emit(json.RawMessage(`"lifetime-1"`), "native-session", false, 0)
	default:
		s.emit(defaultID, "native-session", false, 0)
	}
}

func (s *permissionFixtureState) emit(id json.RawMessage, session string, empty bool, padding int) {
	options := []map[string]string{}
	if !empty {
		for _, choice := range []struct{ id, kind string }{
			{"permit::opaque", "allow_once"}, {"deny::opaque", "reject_once"},
			{"remember::A", "allow_always"}, {"remember::R", "reject_always"},
		} {
			options = append(options, map[string]string{"optionId": choice.id, "name": "private-native-label", "kind": choice.kind})
		}
	}
	tool := map[string]any{"toolCallId": "native-tool"}
	if !empty {
		tool["title"] = "private-native-title"
		tool["rawInput"] = map[string]any{"private": "private-native-command", "padding": strings.Repeat("x", padding)}
		tool["locations"] = []any{map[string]string{"path": "private-unverified-path"}}
	}
	key, err := permissionID(id)
	if err != nil {
		os.Exit(33)
	}
	allowed := map[string]bool{}
	for _, option := range options {
		allowed[option["optionId"]] = true
	}
	s.pending[key] = allowed
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": "session/request_permission",
		"params": map[string]any{"sessionId": session, "toolCall": tool, "options": options}})
	fmt.Println(string(raw))
}

func (s *permissionFixtureState) reply(scenario string, raw, id, result json.RawMessage) {
	var envelope, response, outcome map[string]json.RawMessage
	key, err := permissionID(id)
	allowed, pending := s.pending[key]
	if err != nil || !pending || json.Unmarshal(raw, &envelope) != nil || len(envelope) != 3 ||
		string(envelope["jsonrpc"]) != `"2.0"` || json.Unmarshal(result, &response) != nil || len(response) != 1 ||
		json.Unmarshal(response["outcome"], &outcome) != nil {
		os.Exit(34)
	}
	var selected, optionID string
	if json.Unmarshal(outcome["outcome"], &selected) != nil {
		os.Exit(35)
	}
	if s.cancelled {
		if selected != "cancelled" || len(outcome) != 1 {
			os.Exit(36)
		}
	} else if selected != "selected" || len(outcome) != 2 || json.Unmarshal(outcome["optionId"], &optionID) != nil || !allowed[optionID] {
		os.Exit(37)
	}
	delete(s.pending, key)
	s.responses++
	if scenario == "permission-lifetime" {
		s.emit(json.RawMessage(fmt.Sprintf(`"lifetime-%d"`, s.responses+1)), "native-session", false, 0)
		return
	}
	if scenario == "permission-reuse" {
		s.emit(id, "native-session", false, 0)
		return
	}
	if len(s.pending) == 0 {
		reason := "end_turn"
		if s.cancelled {
			reason = "cancelled"
		}
		s.terminal(reason)
	}
}

func (s *permissionFixtureState) terminal(reason string) {
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": s.promptID, "result": map[string]string{"stopReason": reason}})
	fmt.Println(string(raw))
	s.promptID = nil
}
