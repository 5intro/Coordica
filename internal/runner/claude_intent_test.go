package runner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestClaudeTerminalJSONIntentThroughRealStoppedProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix native-protocol CLI fixture")
	}
	completed := `{"version":1,"status":"completed","requested_paths":[],"reason":"","summary":"Finished"}`
	for _, test := range []struct {
		name, output string
		scope, valid bool
	}{
		{"scope", scopeIntentText(), true, true},
		{"completed", completed, false, true},
		{"prose", "Implementation finished successfully", false, false},
		{"wrong version", strings.Replace(completed, `"version":1`, `"version":2`, 1), false, false},
		{"unknown field", strings.Replace(completed, `"summary":`, `"approved":true,"summary":`, 1), false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newIntentFixture(t)
			f.record.Assignment.Run.Tool, f.record.Assignment.Run.Model = "claude", "provider/flash"
			cli := claudeIntentCLI(t, test.output, !test.scope)
			f.r.config.Executables["claude"] = cli
			f.r.execute = f.r.registry.Execute
			f.perform(context.Background())
			snap := f.snapshot(t)
			run := snap.Runs[0]
			if !run.Quiescent || run.SessionID != "claude-terminal-fixture" || f.completeCalls != 1 {
				t.Fatal("native terminal identity/stop receipt lost", run)
			}
			if test.scope {
				if f.scopeCalls != 1 || len(snap.ScopeRequests) != 1 || f.artifactCalls != 0 || run.State != "stopped" {
					t.Fatal("exact JSON scope handoff failed", snap)
				}
			} else if test.valid {
				if f.artifactCalls != 1 || len(snap.Candidates) != 1 || run.State != "succeeded" {
					t.Fatal("validated JSON and actual committed diff did not produce candidate", snap)
				}
			} else if f.scopeCalls != 0 || f.artifactCalls != 0 || len(snap.Candidates) != 0 || run.State != "failed" {
				t.Fatal("unvalidated terminal text produced scope or material", snap)
			}
		})
	}
}

func claudeIntentCLI(t *testing.T, output string, commit bool) string {
	t.Helper()
	event, err := json.Marshal(map[string]any{
		"type": "result", "subtype": "success", "is_error": false,
		"session_id": "claude-terminal-fixture", "result": output,
	})
	if err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
set -eu
for arg in "$@"; do
  if [ "$arg" = '--input-format' ]; then
    read -r request
    printf '%s\n' '{"type":"control_response","response":{"subtype":"success","request_id":"catalog-1","response":{"models":[{"value":"provider/flash","displayName":"Fixture Flash"}]}}}'
    cat >/dev/null
    exit 0
  fi
  if [ "$arg" = '--json-schema' ]; then exit 42; fi
done
prompt=$(cat)
case "$prompt" in *'"requested_paths"'*) ;; *) exit 43 ;; esac
`
	if commit {
		script += "printf '%s\\n' '// actual fixture change' > test/greeting.test.mjs\n" +
			"git -c user.name=Fixture -c user.email=fixture@example.invalid add test/greeting.test.mjs\n" +
			"git -c user.name=Fixture -c user.email=fixture@example.invalid commit -qm fixture\n"
	}
	script += "printf '%s\\n' '" + strings.ReplaceAll(string(event), "'", "'\"'\"'") + "'\n"
	cli := filepath.Join(t.TempDir(), "fixture-claude")
	if err := os.WriteFile(cli, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return cli
}
