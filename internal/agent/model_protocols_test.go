package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func fakeClaudeCatalog() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			RequestID string `json:"request_id"`
		}
		json.Unmarshal(scanner.Bytes(), &request)
		printJSON(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": request.RequestID, "response": map[string]any{"models": []any{map[string]any{"value": "reported-model", "displayName": "Reported model", "supportedEffortLevels": []string{"high", "max"}}}}}})
	}
}
func fakeConfiguredACP() {
	scanner := bufio.NewScanner(os.Stdin)
	model, effort, mode := "model-a", "high", "code"
	authenticated := false
	options := func() []any {
		return []any{map[string]any{"id": "opaque-mode-option", "category": "mode", "type": "select", "currentValue": mode, "options": []any{map[string]string{"value": "ask", "name": "Ask"}, map[string]string{"value": "code", "name": "Code"}}}, map[string]any{"id": "opaque-model-option", "category": "model", "type": "select", "currentValue": model, "options": []any{map[string]string{"value": "model-a", "name": "Model A"}, map[string]string{"value": "model-b", "name": "Model B"}}}, map[string]any{"id": "opaque-effort-option", "category": "thought_level", "type": "select", "currentValue": effort, "options": []any{map[string]string{"value": "high", "name": "High"}, map[string]string{"value": "max", "name": "Max"}}}}
	}
	for scanner.Scan() {
		var request struct {
			ID     int
			Method string
			Params struct{ ConfigID, Value, MethodID string }
		}
		json.Unmarshal(scanner.Bytes(), &request)
		result := map[string]any{}
		switch request.Method {
		case "initialize":
			result["protocolVersion"] = 1
			result["authMethods"] = []any{map[string]string{"id": "cursor_login"}}
			if os.Getenv("AGENT_TEST_AUTH") == "missing" {
				result["authMethods"] = []any{}
			}
		case "authenticate":
			if request.Params.MethodID != "cursor_login" || os.Getenv("AGENT_TEST_AUTH") == "reject" {
				printJSON(map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": -32000, "message": "auth rejected"}})
				continue
			}
			authenticated = true
		case "session/new":
			if os.Getenv("AGENT_TEST_AUTH") != "" && !authenticated {
				printJSON(map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": -32000, "message": "authenticate first"}})
				continue
			}
			result["sessionId"] = "catalog-session"
			result["configOptions"] = options()
		case "session/set_config_option":
			if request.Params.ConfigID == "opaque-model-option" {
				model = request.Params.Value
			} else if request.Params.ConfigID == "opaque-mode-option" {
				mode = request.Params.Value
			} else {
				effort = request.Params.Value
			}
			if os.Getenv("AGENT_TEST_CATALOG") == "clamp" {
				effort = "high"
			}
			result["configOptions"] = options()
		case "session/prompt":
			recordForbiddenFixtureAction("prompt")
			if os.Getenv("AGENT_TEST_REQUIRE_REVIEW_MODE") == "ask" && mode != "ask" {
				os.Exit(6)
			}
			result["stopReason"] = "end_turn"
		default:
			os.Exit(5)
		}
		printJSON(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}
}
func TestRegisteredProtocolsDiscoverOnlyReportedModelOptions(t *testing.T) {
	for _, tool := range []string{"claude", "kimi", "pi"} {
		t.Run(tool, func(t *testing.T) {
			if tool == "kimi" {
				t.Setenv("AGENT_TEST_MODEL_PROTOCOL", "acp")
			}
			r := DefaultRegistry()
			def, _ := r.Resolve(tool)
			req := fixtureRequest(t, tool, "unused")
			models, err := readToolModels(context.Background(), def, req.Executable)
			if err != nil || len(models) == 0 {
				t.Fatalf("%s %+v %v", tool, models, err)
			}
			if tool == "kimi" && (len(models) != 2 || len(models[1].ReasoningEfforts) != 0) {
				t.Fatal("guessed effort options for a different model")
			}
		})
	}
}
func TestSessionProtocolsApplyExactOptionsBeforePromptAndRejectClamping(t *testing.T) {
	for _, tool := range []string{"kimi", "pi", "claude"} {
		t.Run(tool, func(t *testing.T) {
			req := fixtureRequest(t, tool, "args")
			req.Model = "test/test-model"
			req.ReasoningEffort = "max"
			if tool == "kimi" {
				t.Setenv("AGENT_TEST_MODEL_PROTOCOL", "acp")
				req.Model = "model-b"
			}
			if tool == "claude" {
				req.Model = "reported-model"
			}
			result, err := Execute(context.Background(), req, nil)
			if err != nil || !result.TreeStopped {
				t.Fatalf("%+v %v", result, err)
			}
			if tool == "claude" {
				if !strings.Contains(result.OutputText, "--model") || !strings.Contains(result.OutputText, "reported-model") || !strings.Contains(result.OutputText, "--effort") {
					t.Fatal(result.OutputText)
				}
				return
			}
			path := t.TempDir() + "/prompt"
			t.Setenv("AGENT_TEST_FORBIDDEN_ACTION", "prompt")
			t.Setenv("AGENT_TEST_FORBIDDEN_FILE", path)
			if tool == "kimi" {
				t.Setenv("AGENT_TEST_CATALOG", "clamp")
			} else {
				t.Setenv("AGENT_TEST_MODEL_PROTOCOL", "clamp")
			}
			if _, err = Execute(context.Background(), req, nil); err == nil {
				t.Fatal("clamped settings executed")
			}
			if _, err = os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("prompt sent before exact configuration confirmation")
			}
		})
	}
}
func TestInstalledOtherModelCatalog(t *testing.T) {
	exe := os.Getenv("COORDICA_TEST_OTHER_CATALOG_EXECUTABLE")
	if exe == "" {
		t.Skip("native metadata acceptance is opt-in")
	}
	tool := os.Getenv("COORDICA_TEST_OTHER_CATALOG_TOOL")
	def, ok := DefaultRegistry().Resolve(tool)
	if !ok {
		t.Fatal("unregistered tool")
	}
	models, err := readToolModels(context.Background(), def, exe)
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range models {
		t.Logf("tool=%s model=%s efforts=%v", tool, model.ID, model.ReasoningEfforts)
	}
}

func TestACPModelSelectionRetainsRequiredReviewModeWithModernConfigOptions(t *testing.T) {
	t.Setenv("AGENT_TEST_MODEL_PROTOCOL", "acp")
	t.Setenv("AGENT_TEST_REQUIRE_REVIEW_MODE", "ask")
	req := fixtureRequest(t, "cursor", "success")
	req.Role = "review"
	req.Model = "model-b"
	req.ReasoningEffort = "max"
	result, err := Execute(context.Background(), req, nil)
	if err != nil || !result.TreeStopped {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestACPCatalogAuthenticatesBeforeSessionAndFailsClosed(t *testing.T) {
	for _, auth := range []string{"required", "reject", "missing"} {
		t.Run(auth, func(t *testing.T) {
			t.Setenv("AGENT_TEST_MODEL_PROTOCOL", "acp")
			t.Setenv("AGENT_TEST_AUTH", auth)
			req := fixtureRequest(t, "cursor", "unused")
			def, _ := DefaultRegistry().Resolve("cursor")
			models, e := readToolModels(context.Background(), def, req.Executable)
			if auth == "required" {
				if e != nil || len(models) != 2 {
					t.Fatal("authenticated metadata unavailable", e, models)
				}
			} else if e == nil || len(models) != 0 {
				t.Fatal("failed authentication exposed catalog", e, models)
			}
		})
	}
}
