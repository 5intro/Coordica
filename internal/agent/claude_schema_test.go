package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeStructuredResultNeverFallsBackToProse(t *testing.T) {
	for _, scenario := range []string{"structured-ok", "structured-missing", "structured-null", "structured-string", "structured-array", "structured-failed"} {
		t.Run(scenario, func(t *testing.T) {
			req := fixtureRequest(t, "claude", scenario)
			req.OutputSchemaPath = filepath.Join(t.TempDir(), "review schema.json")
			if err := os.WriteFile(req.OutputSchemaPath, []byte(`{"type":"object"}`), 0600); err != nil {
				t.Fatal(err)
			}
			result, err := Execute(context.Background(), req, nil)
			if scenario == "structured-ok" {
				if err != nil || result.OutputText != `{"answer":"right"}` || !result.TreeStopped {
					t.Fatal(result, err)
				}
			} else if err == nil || result.OutputText != "" || !result.TreeStopped {
				t.Fatal("missing/error structured output became prose approval", result, err)
			}
		})
	}
}

func TestClaudeInvalidOrOversizedSchemaCannotLaunchProcess(t *testing.T) {
	for _, schema := range []string{"", "[]", "{broken", "{}" + strings.Repeat(" ", 64<<10)} {
		req := fixtureRequest(t, "claude", "structured-ok")
		req.OutputSchemaPath = filepath.Join(t.TempDir(), "schema.json")
		if err := os.WriteFile(req.OutputSchemaPath, []byte(schema), 0600); err != nil {
			t.Fatal(err)
		}
		started := false
		_, err := Execute(context.Background(), req, func(event Event) {
			if event.Type == "process.started" {
				started = true
			}
		})
		if err == nil || started {
			t.Fatal("invalid schema launched model", err, started)
		}
	}
}
