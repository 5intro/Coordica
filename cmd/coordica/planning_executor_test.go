package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCodexPlanningAdapterPreservesIdentityAndCancellationEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix synthetic CLI fixture")
	}
	for _, cancelOnStart := range []bool{false, true} {
		t.Run(map[bool]string{false: "quiet receipt", true: "identity persistence failure"}[cancelOnStart], func(t *testing.T) {
			dir := t.TempDir()
			executable := filepath.Join(dir, "fixture-codex")
			script := `#!/bin/sh
model=0
readonly=0
isolated=0
schema=''
previous=''
for arg in "$@"; do
 if [ "$previous" = '--model' ] && [ "$arg" = 'gpt-6.1-sol' ]; then model=1; fi
 if [ "$previous" = '--sandbox' ] && [ "$arg" = 'read-only' ]; then readonly=1; fi
 if [ "$arg" = '--ignore-user-config' ]; then isolated=1; fi
 if [ "$previous" = '--output-schema' ]; then schema="$arg"; fi
 previous="$arg"
done
if [ "$model$readonly$isolated" != '111' ]; then exit 42; fi
if [ ! -f "$schema" ]; then exit 43; fi
cp "$schema" schema-captured.json
printf '%s' "$schema" > schema-path.txt
cat >/dev/null
printf '%s\n' '{"type":"thread.started","thread_id":"fixture-session"}'
printf '%s\n' '{"type":"item.completed","item":{"type":"agent_message","text":"fixture Leader output"}}'
printf '%s\n' '{"type":"turn.completed","usage":{"input_tokens":1}}'
`
			if e := os.WriteFile(executable, []byte(script), 0700); e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			called := false
			persistErr := errors.New("fixture identity persistence failed")
			outcome, e := (CodexPlanningExecutor{Executable: executable}).Execute(ctx, dir, "fixture prompt", func(pid int) error {
				called = true
				if pid <= 0 {
					t.Error("invalid identity")
				}
				if cancelOnStart {
					return persistErr
				}
				return nil
			})
			if !called || !outcome.TreeStopped {
				t.Fatalf("lost process identity/quiet receipt: %+v %v", outcome, e)
			}
			if cancelOnStart {
				if !errors.Is(e, persistErr) || !strings.Contains(outcome.Error, persistErr.Error()) {
					t.Fatal("identity persistence failure not retained", e)
				}
			} else if e != nil || outcome.Output != "fixture Leader output" {
				t.Fatal("quiet output lost", e, outcome.Output)
			}
			if !cancelOnStart {
				captured, err := os.ReadFile(filepath.Join(dir, "schema-captured.json"))
				if err != nil || string(captured) != string(leaderOutputSchema) {
					t.Fatal("CLI did not receive the trusted embedded schema", err)
				}
				path, err := os.ReadFile(filepath.Join(dir, "schema-path.txt"))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(string(path)); !os.IsNotExist(err) {
					t.Fatal("schema scratch was not removed", err)
				}
			}
		})
	}
}

func TestCodexPlanningSchemaSetupFailureHasNoLiveTree(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-scratch-parent")
	t.Setenv("TMPDIR", missing)
	t.Setenv("TMP", missing)
	t.Setenv("TEMP", missing)
	started := false
	outcome, err := (CodexPlanningExecutor{Executable: "must-not-start"}).Execute(context.Background(), t.TempDir(), "fixture", func(int) error {
		started = true
		return nil
	})
	if err == nil || started || !outcome.TreeStopped || outcome.Error == "" {
		t.Fatal("prelaunch schema error fabricated unknown execution", outcome, err)
	}
}

func TestCodexPlanningMissingExecutableHasNoLiveTree(t *testing.T) {
	started := false
	outcome, err := (CodexPlanningExecutor{Executable: filepath.Join(t.TempDir(), "missing-codex")}).Execute(context.Background(), t.TempDir(), "fixture", func(int) error {
		started = true
		return nil
	})
	if err == nil || started || !outcome.TreeStopped || outcome.Error == "" || outcome.ElapsedSeconds > 2 {
		t.Fatal("known prelaunch failure invented a live Agent or full elapsed grant", outcome, err)
	}
}

func TestCodexConsultUsesReplyOnlySchemaAndExplicitReadOnlyModel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix synthetic CLI fixture")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "fake-codex")
	script := `#!/bin/sh
model=0
readonly=0
isolated=0
schema=''
previous=''
for arg in "$@"; do
 if [ "$previous" = '--model' ] && [ "$arg" = 'gpt-6.1-sol' ]; then model=1; fi
 if [ "$previous" = '--sandbox' ] && [ "$arg" = 'read-only' ]; then readonly=1; fi
 if [ "$arg" = '--ignore-user-config' ]; then isolated=1; fi
 if [ "$previous" = '--output-schema' ]; then schema="$arg"; fi
 previous="$arg"
done
if [ "$model$readonly$isolated" != '111' ]; then exit 42; fi
cp "$schema" consult-schema.json
cat >/dev/null
printf '%s\n' '{"type":"thread.started","thread_id":"consult-session"}'
printf '%s\n' '{"type":"item.completed","item":{"type":"agent_message","text":"{\"reply\":\"advice\"}"}}'
printf '%s\n' '{"type":"turn.completed"}'
`
	if e := os.WriteFile(exe, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, e := (CodexPlanningExecutor{Executable: exe}).Execute(ctx, dir, "COORDICA_ADVISORY_V1\nExplicit user consultation", func(int) error { return nil })
	if e != nil || !out.TreeStopped || out.Output != `{"reply":"advice"}` {
		t.Fatal(out, e)
	}
	raw, e := os.ReadFile(filepath.Join(dir, "consult-schema.json"))
	if e != nil || string(raw) != string(consultOutputSchema) || strings.Contains(string(raw), "tasks") {
		t.Fatal("wrong native consult contract", e)
	}
}
