package acpclient

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

// The child is a hand-written ACP simulation, never an installed harness.
func TestMain(m *testing.M) {
	if os.Getenv("COORDICA_ACP_FIXTURE") != "1" {
		os.Exit(m.Run())
	}
	if len(os.Args) != 2 || os.Args[1] != "--offline-acp" || os.Getenv("ACP_EXPLICIT_ENV") != "selected" || os.Getenv("ACP_PARENT_FORBIDDEN") != "" {
		os.Exit(2)
	}
	scenario := os.Getenv("COORDICA_ACP_SCENARIO")
	if scenario == "no-stdin" {
		time.Sleep(30 * time.Second)
		os.Exit(3)
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024), maxFrameBytes)
	for scanner.Scan() {
		f, err := os.OpenFile(os.Getenv("COORDICA_ACP_JOURNAL"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			os.Exit(4)
		}
		_, err = f.Write(append(append([]byte(nil), scanner.Bytes()...), '\n'))
		f.Close()
		if err != nil {
			os.Exit(5)
		}
		var request struct {
			JSONRPC string
			ID      int
			Method  string
			Params  struct {
				Cwd                string
				McpServers         []json.RawMessage
				SessionID          string
				Prompt             []struct{ Type, Text string }
				ProtocolVersion    int
				ClientCapabilities struct {
					FS       struct{ ReadTextFile, WriteTextFile bool }
					Terminal bool
				}
			}
		}
		if json.Unmarshal(scanner.Bytes(), &request) == nil && request.Method == "session/prompt" {
			if request.JSONRPC != "2.0" || request.ID < 3 || request.Params.SessionID != "native-session" || len(request.Params.Prompt) != 1 || request.Params.Prompt[0].Type != "text" || request.Params.Prompt[0].Text == "" {
				os.Exit(11)
			}
			promptFixtureReply(scenario, request.ID, request.Params.Prompt[0].Text)
			continue
		}
		if json.Unmarshal(scanner.Bytes(), &request) == nil && request.Method == "session/new" {
			if request.JSONRPC != "2.0" {
				os.Exit(8)
			}
			sessionFixtureReply(scenario, request.ID, request.Params.Cwd, request.Params.McpServers)
			continue
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil || request.JSONRPC != "2.0" || request.ID != 1 || request.Method != "initialize" || request.Params.ProtocolVersion != 1 ||
			request.Params.ClientCapabilities.Terminal || request.Params.ClientCapabilities.FS.ReadTextFile || request.Params.ClientCapabilities.FS.WriteTextFile {
			os.Exit(6)
		}
		if scenario == "disconnect" {
			os.Exit(0)
		}
		if scenario == "no-response" {
			continue
		}
		result := map[string]any{"protocolVersion": 1, "agentInfo": map[string]string{"name": "offline-acp", "version": "fixture"},
			"agentCapabilities": map[string]any{"loadSession": true, "promptCapabilities": map[string]bool{"image": true}, "mcpCapabilities": map[string]bool{"http": true}, "sessionCapabilities": map[string]any{"_meta": nil}},
			"authMethods":       []any{map[string]string{"id": "offline-auth", "name": "Offline method"}}}
		if scenario == "minimal" {
			result = map[string]any{"protocolVersion": 1, "agentInfo": nil}
		}
		switch scenario {
		case "bad-version":
			result["protocolVersion"] = 2
		case "null-caps":
			result["agentCapabilities"] = nil
		case "null-auth":
			result["authMethods"] = nil
		case "null-bool":
			result["agentCapabilities"] = map[string]any{"loadSession": nil}
		}
		if scenario == "stderr" {
			fmt.Fprintln(os.Stderr, "private-stderr-sentinel")
		}
		response := map[string]any{"jsonrpc": "2.0", "id": 1, "result": result}
		switch scenario {
		case "wrong-id":
			response["id"] = "1"
		case "both":
			response["error"] = map[string]any{"code": -32000, "message": "private-acp-rejection"}
		case "result-array":
			response["result"] = []any{}
		case "rejected":
			delete(response, "result")
			response["error"] = map[string]any{"code": -32000, "message": "private-acp-rejection", "data": map[string]string{"private": "private-acp-rejection"}}
		case "callback":
			response = map[string]any{"jsonrpc": "2.0", "id": 17, "method": "session/request_permission", "params": map[string]any{"private": "private-acp-callback"}}
		case "duplicate-fields":
			fmt.Println(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1,"protocolVersion":2}}`)
			continue
		case "stdout-noise":
			fmt.Println("private-not-json")
			continue
		case "oversize":
			fmt.Println(strings.Repeat("X", maxFrameBytes+1))
			continue
		}
		raw, _ := json.Marshal(response)
		fmt.Println(string(raw))
		if scenario == "duplicate-response" {
			fmt.Println(string(raw))
		}
		if scenario == "rejected" {
			os.Exit(0)
		}
		if scenario == "post-callback" {
			deadline := time.Now().Add(5 * time.Second)
			for {
				if _, err := os.Stat(os.Getenv("COORDICA_ACP_CONTINUE")); err == nil {
					break
				}
				if time.Now().After(deadline) {
					os.Exit(7)
				}
				time.Sleep(5 * time.Millisecond)
			}
			fmt.Println(`{"jsonrpc":"2.0","method":"session/update","params":{"private":"private-acp-callback"}}`)
		}
	}
	os.Exit(0)
}

func sessionFixtureReply(scenario string, id int, directory string, mcp []json.RawMessage) {
	cwd, err := os.Getwd()
	if err != nil || id != 2 || directory != cwd || mcp == nil || len(mcp) != 0 {
		os.Exit(8)
	}
	if scenario == "session-disconnect" {
		os.Exit(0)
	}
	if scenario == "session-no-response" {
		return
	}
	result := map[string]any{"sessionId": "native-session", "modes": nil, "configOptions": nil}
	response := map[string]any{"jsonrpc": "2.0", "id": 2, "result": result}
	if scenario == "updates-early" {
		printUpdateFixture("native-session", map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "private-early"}})
	}
	switch scenario {
	case "session-rejected":
		delete(response, "result")
		response["error"] = map[string]any{"code": -32000, "message": "private-auth-required", "data": map[string]string{"private": "private-auth-required"}}
	case "session-wrong-id":
		response["id"] = 1
	case "session-null-result":
		response["result"] = nil
	case "session-empty-id":
		result["sessionId"] = ""
	case "session-long-id":
		result["sessionId"] = strings.Repeat("x", 513)
	case "session-bad-options":
		result["configOptions"] = true
	case "session-bad-modes":
		result["modes"] = []any{}
	case "session-callback":
		response = map[string]any{"jsonrpc": "2.0", "id": 22, "method": "session/request_permission", "params": map[string]any{"private": "private-session-callback"}}
	case "session-pause":
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := os.Stat(os.Getenv("COORDICA_ACP_CONTINUE")); err == nil {
				break
			}
			if time.Now().After(deadline) {
				os.Exit(9)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	raw, _ := json.Marshal(response)
	fmt.Println(string(raw))
	if scenario == "prompt-blocked-writer" {
		waitFixtureContinue()
		fmt.Println(`{"jsonrpc":"2.0","id":3,"result":{"stopReason":"end_turn"}}`)
		time.Sleep(30 * time.Second)
		os.Exit(12)
	}
	if strings.HasPrefix(scenario, "updates-") && scenario != "updates-early" {
		emitUpdateFixture(scenario)
	}
	if scenario == "updates" {
		for _, update := range []map[string]any{
			{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "private-update-body-秘密"}},
			{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "private-update-body-秘密"}},
			{"sessionUpdate": "tool_call", "toolCallId": "native-tool", "title": "private-tool-title", "status": "in_progress"},
			{"sessionUpdate": "tool_call_update", "toolCallId": "native-tool", "title": nil, "status": nil, "content": nil},
		} {
			raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "native-session", "update": update}})
			fmt.Println(string(raw))
		}
	}
	if scenario == "session-duplicate-response" {
		fmt.Println(string(raw))
	}
}

func promptFixtureReply(scenario string, id int, text string) {
	printUpdateFixture("native-session", map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "private-prompt-output"}})
	printUpdateFixture("native-session", map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "private-prompt-output"}})
	reason := "end_turn"
	if strings.HasPrefix(text, "reason:") {
		reason = strings.TrimPrefix(text, "reason:")
	}
	response := map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"stopReason": reason}}
	switch scenario {
	case "prompt-long":
		time.Sleep(10500 * time.Millisecond)
	case "prompt-pause":
		waitFixtureContinue()
	case "prompt-no-response":
		return
	case "prompt-disconnect":
		os.Exit(0)
	case "prompt-null-result":
		response["result"] = nil
	case "prompt-null-stop":
		response["result"] = map[string]any{"stopReason": nil}
	case "prompt-missing-stop":
		response["result"] = map[string]any{}
	case "prompt-unknown-stop":
		response["result"] = map[string]any{"stopReason": "private-unknown-status"}
	case "prompt-wrong-id":
		response["id"] = id + 100
	case "prompt-old-id":
		if id > 3 {
			response["id"] = id - 1
		}
	case "prompt-rejected":
		if text == "reject" {
			delete(response, "result")
			response["error"] = map[string]any{"code": -32000, "message": "private-prompt-error", "data": map[string]string{"private": "private-prompt-error"}}
		}
	case "prompt-permission":
		fmt.Println(`{"jsonrpc":"2.0","id":22,"method":"session/request_permission","params":{"sessionId":"native-session","private":"private-prompt-callback"}}`)
		return
	case "prompt-overflow":
		for i := 0; i < 33; i++ {
			printUpdateFixture("native-session", map[string]any{"sessionUpdate": "agent_thought_chunk", "content": map[string]string{"type": "text", "text": "private-thought"}})
		}
	}
	raw, _ := json.Marshal(response)
	fmt.Println(string(raw))
	if scenario == "prompt-duplicate" {
		fmt.Println(string(raw))
	}
	if scenario == "prompt-pause" {
		printUpdateFixture("native-session", map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "private-late-session-output"}})
	}
}

func waitFixtureContinue() {
	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := os.Stat(os.Getenv("COORDICA_ACP_CONTINUE")); err == nil {
			return
		}
		if time.Now().After(deadline) {
			os.Exit(13)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func printUpdateFixture(session string, update map[string]any) {
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": session, "update": update}})
	fmt.Println(string(raw))
}

func emitUpdateFixture(scenario string) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(os.Getenv("COORDICA_ACP_CONTINUE")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			os.Exit(10)
		}
		time.Sleep(5 * time.Millisecond)
	}
	update := map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "private-update-payload"}}
	switch scenario {
	case "updates-wrong-session":
		printUpdateFixture("private-other-session", update)
		return
	case "updates-unsupported":
		update = map[string]any{"sessionUpdate": "plan", "entries": []any{}}
	case "updates-image":
		update["content"] = map[string]any{"type": "image", "data": "private-image", "mimeType": "image/png"}
	case "updates-null-content":
		update["content"] = nil
	case "updates-null-text":
		update["content"] = map[string]any{"type": "text", "text": nil}
	case "updates-null-tool-status":
		update = map[string]any{"sessionUpdate": "tool_call", "toolCallId": "tool", "title": "private-title", "status": nil}
	case "updates-bad-tag":
		delete(update, "sessionUpdate")
	case "updates-permission":
		fmt.Println(`{"jsonrpc":"2.0","id":22,"method":"session/request_permission","params":{"sessionId":"native-session","private":"private-callback"}}`)
		return
	case "updates-disconnect":
		os.Exit(0)
	case "updates-optional":
		printUpdateFixture("native-session", map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "tool"})
		printUpdateFixture("native-session", map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "tool", "status": nil})
		return
	case "updates-flood", "updates-byte-flood":
		if scenario == "updates-byte-flood" {
			update = map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": strings.Repeat("x", 512), "status": "pending"}
		}
		for i := 0; i < 33; i++ {
			printUpdateFixture("native-session", update)
		}
		return
	}
	printUpdateFixture("native-session", update)
}

func fixtureConfig(t *testing.T, scenario string) (Config, string) {
	t.Helper()
	dir := t.TempDir()
	journal := filepath.Join(dir, "journal")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	env := []string{"COORDICA_ACP_FIXTURE=1", "COORDICA_ACP_SCENARIO=" + scenario, "COORDICA_ACP_JOURNAL=" + journal, "COORDICA_ACP_CONTINUE=" + filepath.Join(dir, "continue"), "ACP_EXPLICIT_ENV=selected", "GOMAXPROCS=1", "GORACE=atexit_sleep_ms=0"}
	for _, name := range []string{"PATH", "SystemRoot", "TMPDIR", "TMP", "TEMP"} {
		if value := os.Getenv(name); value != "" {
			env = append(env, name+"="+value)
		}
	}
	return Config{Executable: executable, Args: []string{"--offline-acp"}, Directory: dir, Env: env}, journal
}
