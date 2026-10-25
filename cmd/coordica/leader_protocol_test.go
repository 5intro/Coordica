package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"coordica/internal/agent"
	"coordica/internal/domain"
)

const fixtureLeaderModel = `["provider","flash"]`

// A native subprocess speaking ACP exercises the same shared driver as dsh,
// including model confirmation, read-only mode and process-stop receipts.
func TestLeaderProtocolFixture(t *testing.T) {
	args := strings.Join(os.Args, " ")
	if !strings.Contains(args, "coordica-leader-fixture") {
		return
	}
	if os.Getenv("COORDICA_TEST_OWNER_AUTHORITY") != "" {
		os.Exit(6)
	}
	if strings.Contains(args, "fixture-version") {
		fmt.Println("1.0.0")
		os.Exit(0)
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 2<<20)
	model, effort, mode := "other-model", "low", "code"
	options := func() []any {
		option := func(id, category, current string, values ...string) map[string]any {
			choices := []map[string]string{}
			for _, value := range values {
				choices = append(choices, map[string]string{"value": value, "name": value})
			}
			return map[string]any{"id": id, "category": category, "type": "select", "currentValue": current, "options": choices}
		}
		return []any{option("chosen-model", "model", model, "other-model", fixtureLeaderModel),
			option("effort", "thought_level", effort, "low", "high"), option("mode", "mode", mode, "code", "ask")}
	}
	emit := func(value any) { _ = json.NewEncoder(os.Stdout).Encode(value) }
	for scanner.Scan() {
		var request struct {
			ID     int
			Method string
			Params struct {
				ConfigID, Value string
				Prompt          []struct{ Text string }
			}
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			os.Exit(2)
		}
		result := map[string]any{}
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{
				"sessionCapabilities": map[string]any{"close": map[string]any{}, "resume": map[string]any{}}}}
		case "session/new", "session/resume":
			if strings.Contains(args, "fixture-diagnostic") {
				_, _ = os.Stdout.WriteString("[llm] loaded model catalog\n")
			}
			if strings.Contains(args, "fixture-diagnostic-flood") {
				for range 300 {
					_, _ = os.Stdout.WriteString("[llm] diagnostic\n")
				}
			}
			if strings.Contains(args, "fixture-malformed") {
				_, _ = os.Stdout.WriteString("{\"jsonrpc\":\n")
			}
			result = map[string]any{"sessionId": "selected-leader", "configOptions": options()}
		case "session/set_config_option":
			switch request.Params.ConfigID {
			case "chosen-model":
				if !strings.Contains(args, "fixture-mismatch") {
					model = request.Params.Value
				}
			case "effort":
				effort = request.Params.Value
			case "mode":
				mode = request.Params.Value
			default:
				os.Exit(3)
			}
			result["configOptions"] = options()
		case "session/prompt":
			if model != fixtureLeaderModel || effort != "high" || mode != "ask" || len(request.Params.Prompt) != 1 ||
				!strings.Contains(request.Params.Prompt[0].Text, `"reply"`) || !strings.Contains(request.Params.Prompt[0].Text, "without Markdown") {
				os.Exit(4)
			}
			emit(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
				"sessionId": "selected-leader", "update": map[string]any{"sessionUpdate": "agent_message_chunk",
					"content": map[string]string{"type": "text", "text": `{"reply":"selected model, effort and read-only mode verified"}`}}}})
			result["stopReason"] = "end_turn"
		case "session/close":
			emit(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
			os.Exit(0)
		case "session/cancel":
			os.Exit(0)
		default:
			os.Exit(5)
		}
		emit(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}
	os.Exit(0)
}

func TestRegisteredACPLeaderHonorsModelEffortAndStopEvidence(t *testing.T) {
	t.Setenv("COORDICA_TEST_OWNER_AUTHORITY", "must-not-reach-leader")
	for _, mode := range []string{"normal", "diagnostic", "diagnostic-flood", "malformed", "mismatch", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			def := agent.Definition{ID: "custom-leader", Executables: []string{executable}, Protocol: agent.ProtocolACP,
				StartArgs: []string{"-test.run=^TestLeaderProtocolFixture$", "--", "coordica-leader-fixture", "fixture-" + mode},
				Cancel:    true, Review: true, ReviewMode: "ask", CloseMethod: "session/close"}
			registry, err := agent.NewRegistry(def)
			if err != nil {
				t.Fatal(err)
			}
			executor := &AgentPlanningExecutor{Registry: registry}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			persistErr := errors.New("cannot persist Leader identity")
			started := false
			out, err := executor.ExecuteSelected(ctx, domain.LeaderSelection{
				Tool: def.ID, Model: fixtureLeaderModel, ReasoningEffort: "high",
			}, t.TempDir(), "COORDICA_ADVISORY_V1\nRead only consultation", func(int) error {
				started = true
				if mode == "cancel" {
					return persistErr
				}
				return nil
			})
			if !started || !out.TreeStopped {
				t.Fatal("lost start or stop evidence", out, err)
			}
			if mode == "normal" || mode == "diagnostic" {
				if err != nil || !strings.Contains(out.Output, "read-only mode verified") {
					t.Fatal(out, err)
				}
			} else if err == nil || out.Output != "" || mode == "cancel" && !errors.Is(err, persistErr) {
				t.Fatal("unconfirmed configuration executed a prompt or cancellation was lost", out, err)
			}
		})
	}
}
