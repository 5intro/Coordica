package main

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"coordica/internal/domain"
)

func TestEmbeddedLeaderSchemaMatchesDomainContract(t *testing.T) {
	generated := leaderSchema()
	artifact, err := os.ReadFile("../coordica/leader.schema.json")
	if err != nil || !bytes.Equal(artifact, generated) {
		t.Fatal("regenerate embedded Leader schema with contractgen -leader-only", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(generated, &schema); err != nil {
		t.Fatal(err)
	}
	defs := schema["$defs"].(map[string]any)
	types := []any{domain.LeaderResult{}, domain.TaskProposal{}, domain.ScopeProposal{}, domain.MajorDecisionProposal{}}
	if len(defs) != len(types) {
		t.Fatal("unexpected contract refs", defs)
	}
	for _, value := range types {
		typ := reflect.TypeOf(value)
		record := defs[typ.Name()].(map[string]any)
		props := record["properties"].(map[string]any)
		required := record["required"].([]any)
		if record["type"] != "object" || record["additionalProperties"] != false || len(props) != typ.NumField() || len(required) != typ.NumField() {
			t.Fatal("non-strict object", typ.Name())
		}
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if required[i] != name {
				t.Fatal("required field drift", name)
			}
			checkLeaderField(t, props[name].(map[string]any), field.Type, defs)
		}
	}
	if !reflect.DeepEqual(schema["required"], defs["LeaderResult"].(map[string]any)["required"]) || !reflect.DeepEqual(schema["properties"], defs["LeaderResult"].(map[string]any)["properties"]) || schema["type"] != "object" || schema["additionalProperties"] != false {
		t.Fatal("non-strict root")
	}
}

func checkLeaderField(t *testing.T, schema map[string]any, typ reflect.Type, defs map[string]any) {
	t.Helper()
	switch typ.Kind() {
	case reflect.String:
		if schema["type"] != "string" {
			t.Fatal("string drift", schema)
		}
	case reflect.Bool:
		if schema["type"] != "boolean" {
			t.Fatal("bool drift", schema)
		}
	case reflect.Int, reflect.Int64:
		if schema["type"] != "integer" {
			t.Fatal("int drift", schema)
		}
	case reflect.Slice:
		if schema["type"] != "array" {
			t.Fatal("nullable/wrong array", schema)
		}
		checkLeaderField(t, schema["items"].(map[string]any), typ.Elem(), defs)
	case reflect.Struct:
		if schema["$ref"] != "#/$defs/"+typ.Name() || defs[typ.Name()] == nil {
			t.Fatal("ref drift", schema)
		}
	default:
		t.Fatal("unsupported Leader contract type", typ)
	}
}

func TestEmbeddedExecutionSchemaMatchesDomainContract(t *testing.T) {
	b, err := os.ReadFile("../../internal/runner/execution.schema.json")
	if err != nil || !bytes.Equal(b, executionSchema()) {
		t.Fatal("regenerate native execution schema with contractgen -native-only", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(b, &schema); err != nil {
		t.Fatal(err)
	}
	props := schema["properties"].(map[string]any)
	typ := reflect.TypeOf(domain.ExecutionIntent{})
	if schema["$id"] != "https://coordica.local/contracts/execution/v1" || schema["type"] != "object" || schema["additionalProperties"] != false || len(props) != typ.NumField() || len(schema["required"].([]any)) != typ.NumField() {
		t.Fatal("execution schema is not versioned and exact", schema)
	}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		checkLeaderField(t, props[f.Tag.Get("json")].(map[string]any), f.Type, nil)
	}
	if !reflect.DeepEqual(props["status"].(map[string]any)["enum"], []any{"completed", "scope_requested", "blocked"}) || !reflect.DeepEqual(props["version"].(map[string]any)["enum"], []any{float64(domain.ExecutionIntentVersion)}) {
		t.Fatal("execution protocol enum drift")
	}
}

func TestNativeScopeFixtureMatchesVersionedContractAndInsufficientInitialScope(t *testing.T) {
	root := "../../examples/native-scope/"
	b, err := os.ReadFile(root + "expected-scope-intent.json")
	if err != nil {
		t.Fatal(err)
	}
	intent, err := domain.ParseExecutionIntent(string(b))
	if err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(root + "task.json")
	if err != nil {
		t.Fatal(err)
	}
	var task domain.Task
	if err = json.Unmarshal(b, &task); err != nil {
		t.Fatal(err)
	}
	if task.Tool != "codex" || !reflect.DeepEqual(task.AllowedPaths, []string{"test/**"}) || intent.Status != "scope_requested" || !reflect.DeepEqual(intent.RequestedPaths, []string{"src/greeting.mjs"}) {
		t.Fatal("fixture no longer requires native scope extension", task, intent)
	}
	var goal struct {
		AllowedRoots []string `json:"allowed_roots"`
	}
	b, err = os.ReadFile(root + "goal.json")
	if err != nil || json.Unmarshal(b, &goal) != nil || !reflect.DeepEqual(goal.AllowedRoots, []string{"src", "test"}) {
		t.Fatal("fixture goal does not allow ordinary original-task extension", err)
	}
	for _, path := range []string{"src/greeting.mjs", "test/greeting.test.mjs", "tools/verify-acceptance.mjs", "flow.json"} {
		if _, err := os.Stat(root + path); err != nil {
			t.Fatal("source-only fixture missing", path, err)
		}
	}
}
