package appserver

import (
	"encoding/json"
	"fmt"
	"strings"
)

// This is a hand-written, schema-informed child-process fixture, not a captured
// native transcript. It validates the wire targets and counts actual operations.
type controlFixture struct {
	scenario                string
	starts, resumes, turns  int
	active                  bool
	directory, effort, text string
	fallback                bool
	callbacks               int
	replies                 []envelope
	readsWithTurns          int
	readsWithoutTurns       int
}

func (f *controlFixture) handle(frame envelope, reply func(any), notify func(string, any), fail func(int)) {
	var params struct {
		ThreadID       string                        `json:"threadId"`
		TurnID         string                        `json:"turnId"`
		ExpectedTurnID string                        `json:"expectedTurnId"`
		Model          string                        `json:"model"`
		ApprovalPolicy string                        `json:"approvalPolicy"`
		Sandbox        string                        `json:"sandbox"`
		Directory      string                        `json:"cwd"`
		Effort         string                        `json:"effort"`
		Fallback       *bool                         `json:"allowProviderModelFallback"`
		Input          []struct{ Type, Text string } `json:"input"`
		RequestID      json.RawMessage               `json:"requestId"`
		IncludeTurns   *bool                         `json:"includeTurns"`
	}
	if json.Unmarshal(frame.Params, &params) != nil {
		fail(-32602)
		return
	}
	id := fmt.Sprintf("turn-%d", f.turns)
	switch frame.Method {
	case "thread/start", "thread/resume":
		sandboxTypes := map[string]string{"read-only": "readOnly", "workspace-write": "workspaceWrite", "danger-full-access": "dangerFullAccess"}
		approvalOK := params.ApprovalPolicy == "untrusted" || params.ApprovalPolicy == "on-request" || params.ApprovalPolicy == "never"
		if params.Model != "fixture-model" || !approvalOK || sandboxTypes[params.Sandbox] == "" || params.Directory == "" || params.Input != nil {
			fail(-32602)
			return
		}
		if frame.Method == "thread/start" {
			if params.Fallback == nil || *params.Fallback {
				fail(-32602)
				return
			}
			f.starts++
		} else {
			if params.ThreadID != "native-thread" {
				fail(-32001)
				return
			}
			f.resumes++
		}
		f.directory = params.Directory
		config := map[string]any{"thread": fixtureNativeThread(params.Directory),
			"model": params.Model, "modelProvider": "fixture", "cwd": params.Directory,
			"approvalPolicy": params.ApprovalPolicy, "approvalsReviewer": "user", "sandbox": map[string]string{"type": sandboxTypes[params.Sandbox]}}
		switch f.scenario {
		case "typed-model-mismatch":
			config["model"] = "another-model"
		case "typed-directory-mismatch":
			config["cwd"] = params.Directory + "/different"
		case "typed-approval-mismatch":
			config["approvalPolicy"] = "never"
		case "typed-sandbox-mismatch":
			config["sandbox"] = map[string]string{"type": "dangerFullAccess"}
		case "typed-missing-config":
			delete(config, "approvalPolicy")
		case "typed-wrong-resume-id":
			config["thread"] = map[string]any{"id": "different-thread"}
		}
		reply(config)
	case "thread/read":
		if params.ThreadID != "native-thread" || params.IncludeTurns == nil || params.Input != nil {
			fail(-32001)
			return
		}
		if *params.IncludeTurns {
			f.readsWithTurns++
		} else {
			f.readsWithoutTurns++
		}
		reply(map[string]any{"thread": fixtureNativeThread(f.directory)})
	case "turn/start":
		if params.ThreadID != "native-thread" || f.starts+f.resumes == 0 || f.active || len(params.Input) != 1 || params.Input[0].Type != "text" {
			fail(-32001)
			return
		}
		f.turns++
		f.active, f.text, f.effort = true, params.Input[0].Text, params.Effort
		id = fmt.Sprintf("turn-%d", f.turns)
		status := "inProgress"
		if strings.HasPrefix(f.scenario, "typed-status:") {
			status = strings.TrimPrefix(f.scenario, "typed-status:")
			f.active = status == "inProgress"
		}
		turn := fixtureNativeTurn(id, status)
		if f.scenario == "typed-bad-turn" {
			delete(turn, "items")
		}
		reply(map[string]any{"turn": turn})
		notify("turn/started", map[string]any{"threadId": "native-thread", "turn": turn})
	case "turn/steer":
		if params.ThreadID != "native-thread" || params.ExpectedTurnID != id || !f.active || len(params.Input) != 1 {
			fail(-32001)
			return
		}
		if f.scenario == "typed-bad-steer" {
			id = "another-turn"
		}
		reply(map[string]string{"turnId": id})
	case "turn/interrupt", "fixture/finish":
		if !f.active || frame.Method == "turn/interrupt" && (params.ThreadID != "native-thread" || params.TurnID != id) {
			fail(-32001)
			return
		}
		if f.scenario == "typed-interrupt-pending" && frame.Method == "turn/interrupt" {
			reply(map[string]any{})
			return
		}
		f.active = false
		status := "completed"
		if frame.Method == "turn/interrupt" {
			status = "interrupted"
		}
		notify("turn/completed", map[string]any{"threadId": "native-thread",
			"turn": fixtureNativeTurn(id, status)})
		reply(map[string]any{})
	case "fixture/requests", "fixture/malformed-question":
		f.callbacks++
		requestID := 6 + f.callbacks
		question := fixtureQuestion()
		question["turnId"] = id
		question["questions"] = []map[string]any{{"id": "q1", "header": "Fixture", "question": "Choose a fixture value", "isSecret": true, "isOther": true,
			"options": []map[string]string{{"label": "fixture option", "description": "fixture only"}}}}
		if f.scenario == "typed-nonblocking" {
			question["isBlocking"] = false
		}
		if frame.Method == "fixture/malformed-question" {
			question["questions"] = []map[string]string{{"id": "q1", "header": "Fixture", "question": "one"}, {"id": "q1", "header": "Fixture", "question": "two"}}
		}
		fixturePrint(map[string]any{"id": requestID, "method": "item/tool/requestUserInput", "params": question})
		fixturePrint(map[string]any{"id": fmt.Sprint(requestID), "method": "item/commandExecution/requestApproval", "params": map[string]any{
			"threadId": "native-thread", "turnId": id, "itemId": "approval-1", "startedAtMs": 0,
			"command": "fixture only", "availableDecisions": []string{"decline", "cancel"}}})
		reply(map[string]any{})
	case "fixture/file-approval", "fixture/unknown-request":
		method := "item/fileChange/requestApproval"
		if frame.Method == "fixture/unknown-request" {
			method = "item/unknown/request"
		}
		fixturePrint(map[string]any{"id": 125, "method": method, "params": map[string]any{
			"threadId": "native-thread", "turnId": id, "itemId": "file-1", "startedAtMs": 0}})
		reply(map[string]any{})
	case "fixture/resolve":
		requestID := params.RequestID
		if requestID == nil {
			requestID = json.RawMessage(fmt.Sprint(6 + f.callbacks))
		}
		notify("serverRequest/resolved", map[string]any{"threadId": "native-thread", "requestId": requestID})
		reply(map[string]any{})
	case "fixture/pending":
		notify("fixture/pending-registered", map[string]any{})
	case "fixture/status":
		var last json.RawMessage
		if len(f.replies) > 0 {
			last = f.replies[len(f.replies)-1].Result
		}
		reply(map[string]any{"starts": f.starts, "resumes": f.resumes, "turns": f.turns, "active": f.active,
			"cwd": f.directory, "effort": f.effort, "text": f.text, "fallback": f.fallback, "replies": len(f.replies), "lastResult": last,
			"readsWithTurns": f.readsWithTurns, "readsWithoutTurns": f.readsWithoutTurns})
	default:
		fail(-32601)
	}
}

// Required fields and nullable values follow the pinned 0.163.0 schemas linked
// in README.md. These are synthetic data, not native session recordings.
func fixtureNativeThread(directory string) map[string]any {
	return map[string]any{"id": "native-thread", "sessionId": "native-session", "cliVersion": "0.163.0",
		"createdAt": 1, "updatedAt": 1, "cwd": directory, "ephemeral": false, "modelProvider": "fixture",
		"preview": "", "projectId": nil, "source": "appServer", "status": map[string]string{"type": "idle"}, "turns": []any{}}
}

func fixtureNativeTurn(id, status string) map[string]any {
	turn := map[string]any{"id": id, "status": status, "items": []any{}, "error": nil}
	if status == "failed" {
		turn["error"] = map[string]string{"message": "fixture failure"}
	}
	return turn
}
