package appserver

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("COORDICA_APP_SERVER_FIXTURE") == "1" {
		if len(os.Args) != 2 || os.Args[1] != "app-server" {
			os.Exit(2)
		}
		fakeServer()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fixturePrint(value any) {
	raw, _ := json.Marshal(value)
	fmt.Println(string(raw))
}

func fakeServer() {
	reader := bufio.NewScanner(os.Stdin)
	reader.Buffer(make([]byte, 4096), maxFrameBytes+1)
	initialized, created, experimental := false, false, false
	turns, replies := 0, 0
	var reorder []envelope
	for reader.Scan() {
		var frame envelope
		if json.Unmarshal(reader.Bytes(), &frame) != nil {
			os.Exit(3)
		}
		if frame.Method == "" {
			if frame.ID == nil || frame.Result == nil && frame.Error == nil {
				os.Exit(4)
			}
			replies++
			continue
		}
		respond := func(result any) { fixturePrint(map[string]any{"id": frame.ID, "result": result}) }
		notify := func(method string, params any) { fixturePrint(map[string]any{"method": method, "params": params}) }
		fail := func(code int) {
			fixturePrint(map[string]any{"id": frame.ID, "error": RPCError{Code: code, Message: "fixture-private-detail"}})
		}
		if frame.Method == "initialize" {
			if os.Getenv("COORDICA_APP_SERVER_SCENARIO") == "bad-init" {
				respond(map[string]any{})
				continue
			}
			var params struct {
				Capabilities struct {
					Experimental bool `json:"experimentalApi"`
				} `json:"capabilities"`
			}
			if json.Unmarshal(frame.Params, &params) != nil {
				fail(-32602)
				continue
			}
			experimental = params.Capabilities.Experimental
			respond(map[string]string{"userAgent": "codex-fixture/0.163.0", "codexHome": "fixture-only", "platformFamily": "fixture", "platformOs": "fixture"})
			continue
		}
		if frame.Method == "initialized" {
			initialized = true
			continue
		}
		if !initialized {
			os.Exit(5)
		}
		switch frame.Method {
		case "thread/start":
			created = true
			respond(map[string]any{"thread": map[string]string{"id": "native-thread"}})
		case "thread/resume", "thread/read", "fixture/status":
			respond(map[string]any{"threadId": "native-thread", "turns": turns, "replies": replies, "experimental": experimental})
		case "turn/start":
			if !created {
				os.Exit(6)
			}
			turns++
			id := fmt.Sprintf("turn-%d", turns)
			respond(map[string]any{"turn": map[string]string{"id": id, "status": "inProgress"}})
			notify("turn/started", map[string]string{"threadId": "native-thread", "turnId": id})
			if os.Getenv("COORDICA_APP_SERVER_SCENARIO") != "control" {
				data, err := os.ReadFile(filepath.Join(".", "fixture.txt"))
				if err != nil {
					os.Exit(7)
				}
				notify("item/agentMessage/delta", map[string]string{"threadId": "native-thread", "turnId": id, "delta": string(data)})
				notify("turn/completed", map[string]any{"threadId": "native-thread", "turn": map[string]string{"id": id, "status": "completed"}})
			}
		case "turn/steer", "turn/interrupt":
			var params struct {
				ThreadID       string          `json:"threadId"`
				TurnID         string          `json:"turnId"`
				ExpectedTurnID string          `json:"expectedTurnId"`
				Input          json.RawMessage `json:"input"`
			}
			if json.Unmarshal(frame.Params, &params) != nil || frame.Method == "turn/steer" && params.Input == nil {
				fail(-32602)
				continue
			}
			expected := params.ExpectedTurnID
			if frame.Method == "turn/interrupt" {
				expected = params.TurnID
			}
			if params.ThreadID != "native-thread" || expected != fmt.Sprintf("turn-%d", turns) {
				fail(-32001)
				continue
			}
			respond(map[string]string{"turnId": expected})
			if frame.Method == "turn/interrupt" {
				notify("turn/completed", map[string]any{"threadId": "native-thread", "turn": map[string]string{"id": expected, "status": "interrupted"}})
			}
		case "fixture/requests":
			fixturePrint(map[string]any{"id": 7, "method": "item/tool/requestUserInput", "params": fixtureQuestion()})
			fixturePrint(map[string]any{"id": "7", "method": "item/commandExecution/requestApproval", "params": map[string]any{
				"threadId": "native-thread", "turnId": "turn-1", "itemId": "approval-1", "startedAtMs": 0}})
			respond(map[string]any{})
		case "fixture/duplicate-request":
			for range 2 {
				fixturePrint(map[string]any{"id": 42, "method": "item/tool/requestUserInput", "params": fixtureQuestion()})
			}
			respond(map[string]any{})
		case "fixture/reorder":
			reorder = append(reorder, frame)
			if len(reorder) == 2 {
				for i := len(reorder) - 1; i >= 0; i-- {
					fixturePrint(map[string]any{"id": reorder[i].ID, "result": reorder[i].Params})
				}
				reorder = nil
			}
		case "fixture/flood":
			respond(map[string]any{})
			for i := range 256 {
				notify("item/agentMessage/delta", map[string]int{"sequence": i})
			}
		case "fixture/disconnect":
			return
		case "fixture/hang":
			time.Sleep(time.Minute)
		case "fixture/malformed":
			fmt.Printf("{\"id\":%s,\"result\":{},\"result\":{}}\n", frame.ID)
		case "fixture/bad-id":
			fixturePrint(map[string]any{"id": "unknown-id", "result": map[string]any{}})
		case "fixture/oversized":
			fmt.Println(strings.Repeat("x", maxFrameBytes+1))
		default:
			fail(-32601)
		}
	}
}

func fixtureQuestion() map[string]any {
	return map[string]any{"threadId": "native-thread", "turnId": "turn-1", "itemId": "question-1", "isBlocking": true,
		"questions": []map[string]any{{"id": "q1", "header": "Fixture", "question": "Continue?"}}}
}
