package agent

import (
	"context"
	"encoding/json"
	"os"
	"runtime"
	"sort"
	"testing"
	"time"

	"coordica/internal/process"
)

// This test is intentionally opt-in: it invokes an installed authenticated
// CLI and may consume model credits. Ordinary go test never invokes a model.
func TestLiveAgentSmoke(t *testing.T) {
	tool := os.Getenv("COORDICA_LIVE_AGENT")
	if tool == "" {
		t.Skip("set COORDICA_LIVE_AGENT to a registered tool ID to opt in to a real model call")
	}
	if _, known := DefaultRegistry().Resolve(tool); !known {
		t.Fatal("unknown COORDICA_LIVE_AGENT")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cap := Probe(ctx, tool, "")
	if !cap.Available {
		t.Fatalf("%s unavailable: %s", tool, cap.Detail)
	}
	t.Logf("platform=%s/%s version=%s interface preflight; executing explicit live smoke", runtime.GOOS, runtime.GOARCH, cap.Version)
	dir := t.TempDir()
	if _, _, err := process.Collect(ctx, process.Spec{Executable: "git", Args: []string{"init", "-q"}, Dir: dir}, 16<<10); err != nil {
		t.Fatal(err)
	}
	role := os.Getenv("COORDICA_LIVE_ROLE")
	if role == "" {
		role = "review"
	}
	result, err := Execute(ctx, Request{Tool: tool, WorkDir: dir, Role: role, Model: os.Getenv("COORDICA_LIVE_MODEL"),
		ReasoningEffort: os.Getenv("COORDICA_LIVE_EFFORT"), AllowedTools: []string{"Read", "Glob", "Grep"},
		Prompt: `Return exactly one JSON object {"status":"coordica_smoke_ok"}. Do not use tools, edit files, execute commands, browse, or explain.`}, func(event Event) {
		if event.Type == "acp.message" {
			var message struct {
				ID     json.RawMessage
				Method string
				Params struct {
					Update struct {
						Kind string `json:"sessionUpdate"`
					}
				}
				Result map[string]json.RawMessage
			}
			if json.Unmarshal(event.Data, &message) == nil {
				keys := []string{}
				for key := range message.Result {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				t.Logf("ACP envelope method=%s id=%s update=%s result_fields=%v", message.Method, message.ID, message.Params.Update.Kind, keys)
			}
		}
		if event.Type == "error" || event.Type == "turn.failed" {
			safe := SanitizeEvent(event)
			t.Logf("%s: %s", safe.Type, safe.Data)
		}
	})
	if err != nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	var reply struct {
		Status string `json:"status"`
	}
	if json.Unmarshal([]byte(result.OutputText), &reply) != nil || reply.Status != "coordica_smoke_ok" || result.SessionID == "" || !result.TreeStopped {
		t.Fatalf("invalid live result: %+v", result)
	}
}
