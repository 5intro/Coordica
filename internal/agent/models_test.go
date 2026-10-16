package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func fakeModelCatalog() {
	if path := os.Getenv("AGENT_TEST_CATALOG_COUNT"); path != "" {
		f, _ := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if f != nil {
			fmt.Fprintln(f, "start")
			f.Close()
		}
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
			Params struct {
				Cursor string `json:"cursor"`
			} `json:"params"`
		}
		json.Unmarshal(scanner.Bytes(), &request)
		switch request.Method {
		case "initialize":
			printJSON(map[string]any{"id": request.ID, "result": map[string]string{"userAgent": "fixture"}})
		case "initialized":
		case "model/list":
			scenario := os.Getenv("AGENT_TEST_CATALOG")
			if scenario == "timeout" {
				time.Sleep(time.Minute)
				return
			}
			if scenario == "malformed" {
				fmt.Println("not JSON")
				return
			}
			efforts := []map[string]string{{"reasoningEffort": "high"}, {"reasoningEffort": "ultra"}}
			if scenario == "removed" {
				efforts = efforts[:1]
			}
			model := map[string]any{"model": "gpt-6.1-sol", "displayName": "GPT-6.1 Sol", "supportedReasoningEfforts": efforts, "defaultReasoningEffort": "high"}
			if scenario == "alternate" {
				model["model"] = "gpt-6-astra"
			}
			if scenario == "injection" {
				model["model"] = "--model=evil"
			}
			if scenario == "default" {
				model["defaultReasoningEffort"] = "made-up"
			}
			var cursor any
			if scenario == "repeat" || scenario == "pages" && request.Params.Cursor == "" {
				cursor = "next"
			}
			if request.Params.Cursor != "" {
				model["model"] = "another-model"
			}
			printJSON(map[string]any{"id": request.ID, "result": map[string]any{"data": []any{model}, "nextCursor": cursor}})
		default:
			os.Exit(3)
		}
	}
}

func TestModelDiscoveryUsesProtocolAndCachesWithoutAdvertisingExecutionCertification(t *testing.T) {
	t.Setenv("AGENT_TEST_CATALOG", "pages")
	count := t.TempDir() + "/count"
	t.Setenv("AGENT_TEST_CATALOG_COUNT", count)
	r := DefaultRegistry()
	def, _ := r.Resolve("codex")
	cap := r.Probe(context.Background(), "codex", fixtureRequest(t, "codex", "unused").Executable)
	models, status := r.discoverModels(context.Background(), def, cap)
	if status != "reported" || len(models) != 2 || cap.Verified || !reflect.DeepEqual(models[0].ReasoningEfforts, []string{"high", "ultra"}) {
		t.Fatalf("%+v %s %+v", models, status, cap)
	}
	models[0].ReasoningEfforts[0] = "corrupted"
	other, _ := r.discoverModels(context.Background(), def, cap)
	data, _ := os.ReadFile(count)
	if other[0].ReasoningEfforts[0] != "high" || strings.Count(string(data), "start") != 1 {
		t.Fatal("discovery cache is mutable or not reused")
	}
}

func TestModelDiscoveryRejectsMalformedUnboundedAndCanceledReplies(t *testing.T) {
	for _, scenario := range []string{"malformed", "repeat", "injection", "default", "timeout"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("AGENT_TEST_CATALOG", scenario)
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			start := time.Now()
			_, err := codexModels(ctx, fixtureRequest(t, "codex", "unused").Executable)
			if err == nil || time.Since(start) > 4*time.Second {
				t.Fatalf("%v elapsed=%s", err, time.Since(start))
			}
		})
	}
}

func TestExplicitEffortIsRevalidatedAndPassedLiterallyWithoutModelFallback(t *testing.T) {
	r := DefaultRegistry()
	req := fixtureRequest(t, "codex", "args")
	req.Model = "gpt-6.1-sol"
	req.ReasoningEffort = "ultra"
	result, err := r.Execute(context.Background(), req, nil)
	if err != nil || !strings.Contains(result.OutputText, `model_reasoning_effort=\"ultra\"`) {
		t.Fatalf("%+v %v", result, err)
	}
	for _, scenario := range []string{"removed", "malformed"} {
		t.Setenv("AGENT_TEST_CATALOG", scenario)
		started := false
		_, err := r.Execute(context.Background(), req, func(e Event) {
			if e.Type == "process.started" {
				started = true
			}
		})
		if err == nil || started {
			t.Fatalf("unsupported configuration executed: %s %v", scenario, err)
		}
	}
	req.Model = "gpt-6-astra"
	req.ReasoningEffort = ""
	if _, err := r.Execute(context.Background(), req, nil); err == nil {
		t.Fatal("unreported model accepted")
	}
}

func TestSelectedCodexModelReachesNativeArgvWithoutBeingReplacedByDefault(t *testing.T) {
	t.Setenv("AGENT_TEST_CATALOG", "alternate")
	r := DefaultRegistry()
	for _, effort := range []string{"", "ultra"} {
		req := fixtureRequest(t, "codex", "args")
		req.Model, req.ReasoningEffort = "gpt-6-astra", effort
		result, err := r.Execute(context.Background(), req, nil)
		if err != nil || !strings.Contains(result.OutputText, `"--model","gpt-6-astra"`) || strings.Contains(result.OutputText, "gpt-6.1-sol") {
			t.Fatalf("selected model was not used: %+v %v", result, err)
		}
	}
	t.Setenv("AGENT_TEST_CATALOG", "removed")
	started := false
	req := fixtureRequest(t, "codex", "args")
	req.Model = "gpt-6-astra"
	_, err := r.Execute(context.Background(), req, func(e Event) { started = started || e.Type == "process.started" })
	if err == nil || started {
		t.Fatalf("removed model ran or silently fell back: started=%t %v", started, err)
	}
}

// Opt-in: metadata only, no thread/start, prompt, or model turn is sent.
func TestInstalledCodexModelCatalog(t *testing.T) {
	exe := os.Getenv("COORDICA_TEST_CODEX_CATALOG_EXECUTABLE")
	if exe == "" {
		t.Skip("native metadata acceptance is opt-in")
	}
	models, err := codexModels(context.Background(), exe)
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range models {
		if model.ID == "gpt-6.1-sol" {
			t.Logf("native model=%s default=%s efforts=%v", model.ID, model.DefaultReasoningEffort, model.ReasoningEfforts)
			return
		}
	}
	t.Fatal("required model was not reported")
}
