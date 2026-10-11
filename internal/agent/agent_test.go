package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 {
		if os.Args[1] == "--version" {
			fmt.Println("fixture 1.0")
			os.Exit(0)
		}
		if os.Args[1] == "--ask-for-approval" {
			prompt, _ := io.ReadAll(os.Stdin)
			fakeCodex(string(prompt))
			os.Exit(0)
		}
		if os.Args[1] == "-p" {
			prompt, _ := io.ReadAll(os.Stdin)
			fakeClaude(string(prompt))
			os.Exit(0)
		}
		if os.Args[1] == "--profile" {
			fakeACP()
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}
func printJSON(v any) { data, _ := json.Marshal(v); fmt.Println(string(data)) }
func fakeCodex(scenario string) {
	printJSON(map[string]any{"type": "thread.started", "thread_id": "codex-session"})
	if scenario == "cancel" {
		time.Sleep(time.Minute)
		return
	}
	if scenario == "malformed" {
		fmt.Println("terminal display is not JSON")
		return
	}
	if scenario == "incomplete" {
		return
	}
	if scenario == "failed" {
		printJSON(map[string]string{"type": "turn.failed"})
		return
	}
	if scenario == "reconnect" || scenario == "diagnostic-only" {
		printJSON(map[string]any{"type": "error", "message": "Reconnecting... 2/5 (request timed out)"})
		if scenario == "diagnostic-only" {
			return
		}
	}
	text := "done"
	if scenario == "args" {
		raw, _ := json.Marshal(os.Args[1:])
		text = string(raw)
	}
	printJSON(map[string]any{"type": "new.telemetry", "access_token": "fixture-secret"})
	printJSON(map[string]any{"type": "item.completed", "item": map[string]string{"type": "agent_message", "text": text}})
	printJSON(map[string]any{"type": "turn.completed", "usage": map[string]int{"input_tokens": 10}})
}
func fakeClaude(scenario string) {
	printJSON(map[string]any{"type": "system", "subtype": "init", "session_id": "claude-session"})
	if scenario == "cancel" {
		time.Sleep(time.Minute)
		return
	}
	if scenario == "incomplete" {
		return
	}
	if scenario == "malformed" {
		fmt.Println("plain text is not a protocol")
		return
	}
	subtype := "success"
	if scenario == "failed" {
		subtype = "error_during_execution"
	}
	text := "done"
	if scenario == "args" {
		raw, _ := json.Marshal(os.Args[1:])
		text = string(raw)
	}
	printJSON(map[string]any{"type": "result", "subtype": subtype, "is_error": scenario == "failed", "result": text, "session_id": "claude-session"})
}
func fakeACP() {
	scanner := bufio.NewScanner(os.Stdin)
	scenario := ""
	for scanner.Scan() {
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
		}
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			os.Exit(4)
		}
		respond := func(result any) { printJSON(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": result}) }
		switch msg.Method {
		case "initialize":
			respond(map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{"sessionCapabilities": map[string]any{"resume": map[string]any{}}}})
		case "session/new":
			respond(map[string]string{"sessionId": "dsh-session"})
			if os.Getenv("AGENT_TEST_STALL_STDIN") == "yes" {
				time.Sleep(time.Minute)
				return
			}
		case "session/resume":
			respond(map[string]any{})
		case "session/prompt":
			var params struct {
				SessionID string `json:"sessionId"`
				Prompt    []struct {
					Text string `json:"text"`
				} `json:"prompt"`
			}
			json.Unmarshal(msg.Params, &params)
			scenario = params.Prompt[0].Text
			if scenario == "cancel" {
				continue
			}
			if scenario == "permission" {
				printJSON(map[string]any{"jsonrpc": "2.0", "id": "permission-1", "method": "session/request_permission", "params": map[string]any{"sessionId": params.SessionID, "toolCall": map[string]string{"toolCallId": "opaque-call"}, "options": []map[string]string{{"kind": "allow_once", "optionId": "allow-once"}}}})
				continue
			}
			if scenario == "wrong-id" {
				printJSON(map[string]any{"jsonrpc": "2.0", "id": 900, "result": map[string]string{"stopReason": "end_turn"}})
				continue
			}
			printJSON(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": params.SessionID, "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "done"}}}})
			respond(map[string]string{"stopReason": "end_turn"})
		case "session/cancel":
			printJSON(map[string]any{"jsonrpc": "2.0", "id": 3, "result": map[string]string{"stopReason": "cancelled"}})
		case "session/close":
			respond(map[string]any{})
		case "":
			if string(msg.ID) == `"permission-1"` {
				if strings.Contains(string(msg.Result), "selected") {
					os.Exit(9)
				}
				printJSON(map[string]any{"jsonrpc": "2.0", "id": 3, "result": map[string]string{"stopReason": "refusal"}})
			}
		}
	}
}

func fixtureRequest(t *testing.T, tool, prompt string) Request {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Request{Tool: tool, Executable: exe, WorkDir: t.TempDir(), Prompt: prompt}
}
func TestDocumentedJSONProtocols(t *testing.T) {
	for _, tool := range []string{"codex", "claude", "dsh"} {
		t.Run(tool, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var events []Event
			result, err := Execute(ctx, fixtureRequest(t, tool, "success"), func(event Event) { events = append(events, event) })
			if err != nil || result.OutputText != "done" || result.SessionID == "" || !result.TreeStopped {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if len(events) == 0 {
				t.Fatal("no structured events")
			}
			for _, e := range events {
				if strings.Contains(string(e.Data), "fixture-secret") {
					t.Fatal("named secret field leaked")
				}
			}
		})
	}
}
func TestExitZeroWithoutVerifiedTerminalFails(t *testing.T) {
	for _, tool := range []string{"codex", "claude"} {
		for _, scenario := range []string{"incomplete", "malformed", "failed"} {
			t.Run(tool+"/"+scenario, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_, err := Execute(ctx, fixtureRequest(t, tool, scenario), nil)
				if err == nil {
					t.Fatal("unverified success accepted")
				}
			})
		}
	}
}

func TestCodexReconnectDiagnosticCanRecoverButCannotReplaceTerminal(t *testing.T) {
	for _, scenario := range []string{"reconnect", "diagnostic-only"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result, err := Execute(ctx, fixtureRequest(t, "codex", scenario), nil)
			if scenario == "reconnect" {
				if err != nil || result.OutputText != "done" || !result.TreeStopped {
					t.Fatalf("recovered diagnostic rejected: %+v %v", result, err)
				}
			} else if err == nil {
				t.Fatal("error diagnostic alone replaced a terminal result")
			}
		})
	}
}
func TestCancellationAcrossProtocols(t *testing.T) {
	for _, tool := range []string{"codex", "claude", "dsh"} {
		t.Run(tool, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			result, err := Execute(ctx, fixtureRequest(t, tool, "cancel"), nil)
			if !errors.Is(err, context.DeadlineExceeded) || !result.TreeStopped {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}
func TestACPRejectsOpaquePermissionAndUnexpectedReply(t *testing.T) {
	for _, scenario := range []string{"permission", "wrong-id"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			denied := false
			_, err := Execute(ctx, fixtureRequest(t, "dsh", scenario), func(e Event) { denied = denied || e.Type == "permission_denied" })
			if err == nil || (scenario == "permission" && !denied) {
				t.Fatalf("err=%v denied=%v", err, denied)
			}
		})
	}
}
func TestResumeUsesExactSession(t *testing.T) {
	for _, tool := range []string{"codex", "claude", "dsh"} {
		t.Run(tool, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			req := fixtureRequest(t, tool, "args")
			req.ResumeSession = "exact-session"
			result, err := Execute(ctx, req, nil)
			if err != nil {
				t.Fatal(err)
			}
			if tool == "dsh" {
				if result.SessionID != "exact-session" {
					t.Fatal(result.SessionID)
				}
			} else if !strings.Contains(result.OutputText, "exact-session") {
				t.Fatal("resume did not pass exact session")
			}
		})
	}
}
func TestReadOnlyReviewerAndNoUnconditionalBypass(t *testing.T) {
	for _, tool := range []string{"codex", "claude"} {
		t.Run(tool, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			req := fixtureRequest(t, tool, "args")
			req.Role = "review"
			req.AllowedTools = []string{"Read", "Bash"}
			result, err := Execute(ctx, req, nil)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(result.OutputText, "bypass") {
				t.Fatal("approval bypass")
			}
			if tool == "codex" && !strings.Contains(result.OutputText, "read-only") {
				t.Fatal("missing read-only sandbox")
			}
			if tool == "claude" && !strings.Contains(result.OutputText, "Read,Glob,Grep") {
				t.Fatal("review tools unrestricted")
			}
		})
	}
}
func TestProbeIsPreflightNotCertification(t *testing.T) {
	req := fixtureRequest(t, "codex", "unused")
	cap := Probe(context.Background(), req.Tool, req.Executable)
	if !cap.Available || cap.Verified || cap.Version != "fixture 1.0" {
		t.Fatalf("%+v", cap)
	}
}

func TestLargePromptsUseStdin(t *testing.T) {
	for _, tool := range []string{"codex", "claude"} {
		t.Run(tool, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			req := fixtureRequest(t, tool, strings.Repeat("large task instruction ", 4096))
			for _, arg := range commandArgs(req) {
				if arg == req.Prompt {
					t.Fatal("prompt present in argv")
				}
			}
			result, err := Execute(ctx, req, nil)
			if err != nil || result.OutputText != "done" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestPublicationAuthorityIsNotInjectedAndEventStringsAreRedacted(t *testing.T) {
	t.Setenv("GH_TOKEN", "fixture-publish-authority")
	t.Setenv("COORDICA_OWNER_TOKEN", "fixture-center-authority")
	t.Setenv("OPENAI_API_KEY", "fixture-model-authority")
	env := strings.Join(developmentEnv(), "\n")
	if strings.Contains(env, "fixture-publish-authority") || strings.Contains(env, "fixture-center-authority") {
		t.Fatal("publication or center authority injected")
	}
	if !strings.Contains(env, "OPENAI_API_KEY=fixture-model-authority") {
		t.Fatal("official model authentication discarded")
	}
	safe := SanitizeEvent(Event{Type: "error", Data: json.RawMessage(`{"message":"fixture-publish-authority fixture-center-authority fixture-model-authority node-authority","api_key":"unknown-sensitive-value"}`)}, "node-authority")
	for _, secret := range []string{"fixture-publish-authority", "fixture-center-authority", "fixture-model-authority", "node-authority", "unknown-sensitive-value"} {
		if strings.Contains(string(safe.Data), secret) {
			t.Fatal("secret retained in structured event")
		}
	}
}

func TestACPCancellationCannotBlockBehindFullInputPipe(t *testing.T) {
	t.Setenv("AGENT_TEST_STALL_STDIN", "yes")
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	req := fixtureRequest(t, "dsh", strings.Repeat("large input ", 100000))
	started := time.Now()
	result, err := Execute(ctx, req, nil)
	if !errors.Is(err, context.DeadlineExceeded) || !result.TreeStopped || time.Since(started) > 3*time.Second {
		t.Fatalf("unbounded input cancellation: %+v %v", result, err)
	}
}
