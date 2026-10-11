package agent

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

func fakePi() {
	scanner := bufio.NewScanner(os.Stdin)
	session := "pi-session-file"
	for i, arg := range os.Args {
		if arg == "--session" && i+1 < len(os.Args) {
			session = os.Args[i+1]
		}
	}
	scenario := ""
	for scanner.Scan() {
		var msg struct{ ID, Type, Message string }
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			os.Exit(4)
		}
		response := func(data any) {
			printJSON(map[string]any{"type": "response", "id": msg.ID, "command": msg.Type, "success": true, "data": data})
		}
		switch msg.Type {
		case "get_state":
			response(map[string]any{"sessionId": "pi-fixture-id", "sessionFile": session, "isStreaming": scenario == "still-streaming", "isCompacting": false, "pendingMessageCount": 0})
		case "prompt":
			recordForbiddenFixtureAction("prompt")
			scenario = msg.Message
			response(nil)
			printJSON(map[string]any{"type": "message_start", "message": map[string]string{"role": "user", "content": msg.Message}})
			printJSON(map[string]any{"type": "message_end", "message": map[string]string{"role": "user", "content": msg.Message}})
			if scenario == "cancel" {
				continue
			}
			if scenario == "incomplete" {
				os.Exit(0)
			}
			if scenario == "malformed" {
				println("not a protocol")
				os.Stdout.WriteString("not a protocol\n")
				os.Exit(0)
			}
			if scenario == "ui" {
				printJSON(map[string]any{"type": "extension_ui_request", "id": "notify-ui", "method": "notify", "message": "status only"})
				printJSON(map[string]any{"type": "extension_ui_request", "id": "confirm-ui", "method": "confirm", "title": "owner decision"})
			}
			stop := "stop"
			if scenario == "failed" {
				stop = "error"
			}
			text := "done"
			if scenario == "args" {
				raw, _ := json.Marshal(os.Args[1:])
				text = string(raw)
			}
			printJSON(map[string]any{"type": "message_end", "message": map[string]any{"role": "assistant", "content": []map[string]string{{"type": "text", "text": text}}, "stopReason": stop}})
			printJSON(map[string]any{"type": "agent_end"})
			if scenario == "only-agent-end" {
				os.Exit(0)
			}
			if scenario == "retry-after-agent-end" {
				printJSON(map[string]any{"type": "auto_retry_start"})
				printJSON(map[string]any{"type": "message_end", "message": map[string]any{"role": "assistant", "content": []map[string]string{{"type": "text", "text": "retried"}}, "stopReason": "stop"}})
			}
			if scenario == "no-aborted-field" {
				printJSON(map[string]any{"type": "agent_settled"})
			} else {
				printJSON(map[string]any{"type": "agent_settled", "aborted": scenario == "aborted"})
			}
		case "clear_queue":
			response(nil)
		case "abort":
			response(nil)
			printJSON(map[string]any{"type": "agent_settled", "aborted": true})
		case "extension_ui_response":
			if msg.ID == "notify-ui" {
				os.Exit(8)
			}
		default:
			if !strings.Contains(msg.Type, "extension") {
				os.Exit(7)
			}
		}
	}
}
