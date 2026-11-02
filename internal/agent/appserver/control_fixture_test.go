package appserver

import (
	"encoding/json"
	"fmt"
)

// This is a hand-written, schema-informed child-process fixture, not a captured
// native transcript. It validates the wire targets and counts actual operations.
type controlFixture struct {
	scenario                string
	starts, resumes, turns  int
	active                  bool
	directory, effort, text string
	fallback                bool
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
	}
	if json.Unmarshal(frame.Params, &params) != nil {
		fail(-32602)
		return
	}
	id := fmt.Sprintf("turn-%d", f.turns)
	switch frame.Method {
	case "thread/start", "thread/resume":
		if params.Model != "fixture-model" || params.ApprovalPolicy != "on-request" || params.Sandbox != "read-only" || params.Directory == "" {
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
		config := map[string]any{"thread": map[string]any{"id": "native-thread", "turns": []any{}},
			"model": params.Model, "modelProvider": "fixture", "cwd": params.Directory,
			"approvalPolicy": params.ApprovalPolicy, "approvalsReviewer": "user", "sandbox": map[string]string{"type": "readOnly"}}
		switch f.scenario {
		case "typed-model-mismatch":
			config["model"] = "another-model"
		case "typed-directory-mismatch":
			config["cwd"] = params.Directory + "/different"
		case "typed-missing-config":
			delete(config, "approvalPolicy")
		case "typed-wrong-resume-id":
			config["thread"] = map[string]any{"id": "different-thread"}
		}
		reply(config)
	case "thread/read":
		if params.ThreadID != "native-thread" {
			fail(-32001)
			return
		}
		reply(map[string]any{"thread": map[string]any{"id": "native-thread", "turns": []any{}}})
	case "turn/start":
		if params.ThreadID != "native-thread" || f.starts+f.resumes == 0 || f.active || len(params.Input) != 1 || params.Input[0].Type != "text" {
			fail(-32001)
			return
		}
		f.turns++
		f.active, f.text, f.effort = true, params.Input[0].Text, params.Effort
		id = fmt.Sprintf("turn-%d", f.turns)
		turn := map[string]any{"id": id, "status": "inProgress", "items": []any{}}
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
		f.active = false
		reply(map[string]any{})
		status := "completed"
		if frame.Method == "turn/interrupt" {
			status = "interrupted"
		}
		notify("turn/completed", map[string]any{"threadId": "native-thread",
			"turn": map[string]any{"id": id, "status": status, "items": []any{}}})
	case "fixture/status":
		reply(map[string]any{"starts": f.starts, "resumes": f.resumes, "turns": f.turns, "active": f.active,
			"cwd": f.directory, "effort": f.effort, "text": f.text, "fallback": f.fallback})
	default:
		fail(-32601)
	}
}
