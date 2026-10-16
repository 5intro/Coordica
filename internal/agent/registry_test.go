package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCustomToolReusesProtocolWithoutBrandChanges(t *testing.T) {
	def, _ := DefaultRegistry().Resolve("kimi")
	def.ID = "my-acp-agent"
	def.Aliases = []string{"my-agent"}
	r, err := NewRegistry(def)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := r.Execute(ctx, fixtureRequest(t, "my-agent", "success"), nil)
	if err != nil || result.OutputText != "done" || !result.TreeStopped {
		t.Fatalf("%+v %v", result, err)
	}
	if cap := r.Probe(ctx, "my-agent", fixtureRequest(t, "my-agent", "unused").Executable); cap.Tool != "my-acp-agent" || cap.Protocol != ProtocolACP || !cap.Available {
		t.Fatalf("%+v", cap)
	}
	if _, ok := DefaultRegistry().Resolve(def.ID); ok {
		t.Fatal("extension polluted another registry")
	}
}

func TestRelativeExecutableIsResolvedBeforeWorkingDirectoryChanges(t *testing.T) {
	req := fixtureRequest(t, "kimi", "success")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(cwd, req.Executable)
	if err != nil {
		t.Skipf("executable is on another volume: %v", err)
	}
	req.Executable = rel
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := Execute(ctx, req, nil)
	if err != nil || result.OutputText != "done" {
		t.Fatalf("relative executable moved with working directory: %+v %v", result, err)
	}
}

func TestDiscoveryFindsRegisteredCLIOutsidePATHAndHonorsExplicitMissingOverride(t *testing.T) {
	cli := filepath.Join(t.TempDir(), "Desktop Harness.app", "Contents", "cli", "harness.exe")
	if err := os.MkdirAll(filepath.Dir(cli), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cli, []byte("fixture executable"), 0700); err != nil {
		t.Fatal(err)
	}
	def := Definition{Executables: []string{filepath.Join(t.TempDir(), "not-in-PATH"), cli}}
	if path, err := resolveExecutable(def, ""); err != nil || path != cli {
		t.Fatalf("bundled CLI outside PATH not found: %s %v", path, err)
	}
	if _, err := resolveExecutable(def, filepath.Join(t.TempDir(), "explicitly-disabled")); err == nil {
		t.Fatal("explicit missing executable fell back to a bundled tool")
	}
}

func TestRegistryRejectsAmbiguousOrInvalidDefinitions(t *testing.T) {
	base, _ := DefaultRegistry().Resolve("kimi")
	for _, change := range []func(*Definition){
		func(d *Definition) { d.ID = "../agent" }, func(d *Definition) { d.Protocol = "terminal-prose" }, func(d *Definition) { d.Executables = nil }, func(d *Definition) { d.Aliases = []string{d.ID} }, func(d *Definition) { d.StartArgs = []string{"{untrusted}"} }, func(d *Definition) { d.ResumeMethod = "session/guess" }, func(d *Definition) { d.CloseMethod = "shutdown" }, func(d *Definition) { d.Protocol = ProtocolPi },
	} {
		def := cloneDefinition(base)
		change(&def)
		if _, err := NewRegistry(def); err == nil {
			t.Fatalf("invalid definition accepted: %+v", def)
		}
	}
	r := DefaultRegistry()
	if err := r.Register(base); err == nil {
		t.Fatal("duplicate ID accepted")
	}
	base.ID = "other"
	base.Aliases = []string{"claude-code"}
	if err := r.Register(base); err == nil {
		t.Fatal("duplicate alias accepted")
	}
	defs := r.Definitions()
	defs[0].StartArgs[0] = "modified"
	actual, _ := r.Resolve("codex")
	if actual.StartArgs[0] == "modified" {
		t.Fatal("caller mutated registry state")
	}
}

func TestResumeUnsupportedIsRejectedBeforeStartingProcess(t *testing.T) {
	def, _ := DefaultRegistry().Resolve("kimi")
	def.Resume = false
	r, err := NewRegistry(def)
	if err != nil {
		t.Fatal(err)
	}
	req := fixtureRequest(t, "kimi", "success")
	req.ResumeSession = "stored-session"
	started := false
	_, err = r.Execute(context.Background(), req, func(e Event) { started = started || e.Type == "process.started" })
	if err == nil || started {
		t.Fatalf("err=%v started=%v", err, started)
	}
}

func TestACPResumeAndReviewNeedAdvertisedCapabilities(t *testing.T) {
	for _, tool := range []string{"dsh", "cursor", "kimi"} {
		t.Run(tool, func(t *testing.T) {
			t.Setenv("AGENT_TEST_ACP_CAPS", "no-resume")
			req := fixtureRequest(t, tool, "success")
			req.ResumeSession = "stored-session"
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, err := Execute(ctx, req, nil)
			if err == nil || !strings.Contains(err.Error(), "advertise") {
				t.Fatalf("%v", err)
			}
		})
	}
	t.Run("review-mode", func(t *testing.T) {
		t.Setenv("AGENT_TEST_ACP_CAPS", "no-mode")
		req := fixtureRequest(t, "cursor", "success")
		req.Role = "review"
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, err := Execute(ctx, req, nil)
		if err == nil || !strings.Contains(err.Error(), "review mode") {
			t.Fatalf("%v", err)
		}
	})
}

func TestACPWithoutCloseUsesEOFAndConfirmedProcessStop(t *testing.T) {
	t.Setenv("AGENT_TEST_ACP_CAPS", "no-close")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := Execute(ctx, fixtureRequest(t, "kimi", "success"), nil)
	if err != nil || !result.TreeStopped {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestStructuredACPPermissionMapping(t *testing.T) {
	for _, tool := range []string{"dsh", "cursor", "kimi"} {
		for _, scenario := range []string{"permission-read", "permission-edit", "permission-outside"} {
			t.Run(tool+"/"+scenario, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				req := fixtureRequest(t, tool, scenario)
				req.AllowedTools = []string{"Read", "Edit", "Write"}
				result, err := Execute(ctx, req, nil)
				if scenario == "permission-outside" {
					if err == nil {
						t.Fatal("outside write allowed")
					}
					return
				}
				if err != nil || result.OutputText != "done" {
					t.Fatalf("%+v %v", result, err)
				}
			})
		}
	}
}

func TestReviewAndSymlinkCannotBroadenACPPermissions(t *testing.T) {
	req := fixtureRequest(t, "kimi", "unused")
	req.Role = "review"
	req.AllowedTools = []string{"Edit", "Write", "Bash", "Read"}
	permission := func(kind, path string) json.RawMessage {
		data, _ := json.Marshal(map[string]any{"sessionId": "session", "toolCall": map[string]any{"toolCallId": "call", "kind": kind, "locations": []map[string]string{{"path": path}}}, "options": []map[string]string{{"kind": "allow_once", "optionId": "once"}}})
		return data
	}
	path := filepath.Join(req.WorkDir, "review.txt")
	for _, kind := range []string{"edit", "execute", "delete"} {
		if _, allowed := acpPermission(req, "session", permission(kind, path)); allowed {
			t.Fatalf("review granted %s", kind)
		}
	}
	if _, allowed := acpPermission(req, "session", permission("read", path)); !allowed {
		t.Fatal("review read denied")
	}
	out := t.TempDir()
	link := filepath.Join(req.WorkDir, "escape")
	if err := os.Symlink(out, link); err != nil {
		t.Logf("symlink fixture unavailable: %v", err)
		return
	}
	if _, allowed := acpPermission(req, "session", permission("read", filepath.Join(link, "private.txt"))); allowed {
		t.Fatal("symlink escape granted")
	}
}

func TestPiWaitsForSettledIdleAndSuccessfulAssistant(t *testing.T) {
	for _, scenario := range []string{"only-agent-end", "retry-after-agent-end", "still-streaming", "no-aborted-field", "aborted"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			result, err := Execute(ctx, fixtureRequest(t, "pi", scenario), nil)
			if scenario == "retry-after-agent-end" {
				if err != nil || result.OutputText != "retried" {
					t.Fatalf("%+v %v", result, err)
				}
			} else if err == nil {
				t.Fatalf("unsettled pi result accepted: %+v", result)
			}
		})
	}
}

func TestPiOlderProtocolBaselineIsUnavailable(t *testing.T) {
	t.Setenv("AGENT_TEST_VERSION", "pi 0.80.4")
	req := fixtureRequest(t, "pi", "success")
	cap := Probe(context.Background(), "pi", req.Executable)
	if cap.Available || !strings.Contains(cap.Detail, "1.1.0") {
		t.Fatalf("%+v", cap)
	}
	started := false
	_, err := Execute(context.Background(), req, func(e Event) { started = started || e.Type == "process.started" })
	if err == nil || started {
		t.Fatalf("old RPC protocol launched: %v %v", err, started)
	}
}

func TestPiReviewDisablesMutationAndExtensionTools(t *testing.T) {
	req := fixtureRequest(t, "pi", "args")
	req.Role = "review"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := Execute(ctx, req, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range []string{"read,grep,find,ls", "--no-extensions", "--no-skills", "--no-prompt-templates", "--no-mcp"} {
		if !strings.Contains(result.OutputText, arg) {
			t.Fatalf("missing %s: %s", arg, result.OutputText)
		}
	}
}

func TestACPPartialPermissionUsesOnlySameCallTypedMetadata(t *testing.T) {
	req := fixtureRequest(t, "kimi", "unused")
	req.AllowedTools = []string{"Write"}
	path := filepath.Join(req.WorkDir, "file.txt")
	calls := map[string]acpMetadata{}
	metadata, _ := json.Marshal(map[string]any{"toolCallId": "call", "kind": "edit", "locations": []map[string]string{{"path": path}}, "content": []any{}})
	if err := recordACPCall(calls, metadata); err != nil {
		t.Fatal(err)
	}
	permission := func(id, session, kind string) json.RawMessage {
		call := map[string]string{"toolCallId": id, "title": "irrelevant"}
		if kind != "" {
			call["kind"] = kind
		}
		raw, _ := json.Marshal(map[string]any{"sessionId": session, "toolCall": call, "options": []map[string]string{{"kind": "allow_once", "optionId": "safe-once"}}})
		return raw
	}
	if option, allowed := acpPermissionFromCalls(req, "session", permission("call", "session", ""), calls); !allowed || option != "safe-once" {
		t.Fatal("typed metadata was not reused")
	}
	for _, raw := range []json.RawMessage{permission("different", "session", ""), permission("call", "other-session", ""), permission("call", "session", "execute")} {
		if _, allowed := acpPermissionFromCalls(req, "session", raw, calls); allowed {
			t.Fatal("conflicting or unrelated permission approved")
		}
	}
	conflict, _ := json.Marshal(map[string]any{"toolCallId": "call", "kind": "edit", "locations": []map[string]string{{"path": filepath.Join(t.TempDir(), "outside.txt")}}})
	if err := recordACPCall(calls, conflict); err != nil {
		t.Fatal(err)
	}
	if _, allowed := acpPermissionFromCalls(req, "session", permission("call", "session", ""), calls); allowed {
		t.Fatal("conflicting notification broadened permission")
	}
}

func TestCancellationWhileHandshakeReplyIsReadyCannotLaunchWork(t *testing.T) {
	for _, scenario := range []string{"initialize", "new", "permission", "pi-state"} {
		t.Run(scenario, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "forbidden-action")
			action := "prompt"
			if scenario == "initialize" {
				action = "new"
			}
			if scenario == "permission" {
				action = "allow"
			}
			t.Setenv("AGENT_TEST_FORBIDDEN_ACTION", action)
			t.Setenv("AGENT_TEST_FORBIDDEN_FILE", file)
			tool, prompt := "kimi", "success"
			if scenario == "pi-state" {
				tool = "pi"
			}
			if scenario == "permission" {
				prompt = "permission-edit"
			}
			req := fixtureRequest(t, tool, prompt)
			req.AllowedTools = []string{"Write"}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			guard := time.AfterFunc(5*time.Second, cancel)
			defer guard.Stop()
			result, err := Execute(ctx, req, func(e Event) {
				data := string(e.Data)
				if (scenario == "initialize" && strings.Contains(data, "protocolVersion")) || (scenario == "new" && strings.Contains(data, `"sessionId":"dsh-session"`)) || (scenario == "permission" && strings.Contains(data, "session/request_permission")) || (scenario == "pi-state" && strings.Contains(data, `"command":"get_state"`)) {
					cancel()
				}
			})
			if !errors.Is(err, context.Canceled) || !result.TreeStopped {
				t.Fatalf("%+v %v", result, err)
			}
			if data, err := os.ReadFile(file); !os.IsNotExist(err) {
				t.Fatalf("work occurred after cancellation: %s %v", data, err)
			}
		})
	}
}

func TestACPResumeExcludesReplayedHistory(t *testing.T) {
	for _, tool := range []string{"dsh", "cursor", "kimi"} {
		for _, prompt := range []string{"success", "permission-edit"} {
			t.Run(tool+"/"+prompt, func(t *testing.T) {
				req := fixtureRequest(t, tool, prompt)
				req.ResumeSession = "prior-session"
				req.AllowedTools = []string{"Write"}
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				result, err := Execute(ctx, req, nil)
				if err != nil || result.OutputText != "done" {
					t.Fatalf("history polluted current result/authorization: %+v %v", result, err)
				}
			})
		}
	}
}

func TestPiFireAndForgetUIIsNotARequestForOwnerInput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	blocked := 0
	result, err := Execute(ctx, fixtureRequest(t, "pi", "ui"), func(e Event) {
		if e.Type == "input_required" {
			blocked++
		}
	})
	if err != nil || result.OutputText != "done" || blocked != 1 {
		t.Fatalf("%+v %v blocked=%d", result, err, blocked)
	}
}

func TestPiResumeCannotRebindItsRecordedWorkingDirectory(t *testing.T) {
	req := fixtureRequest(t, "pi", "success")
	req.ResumeSession = filepath.Join(t.TempDir(), "session.jsonl")
	header, _ := json.Marshal(map[string]string{"type": "session", "cwd": t.TempDir()})
	if err := os.WriteFile(req.ResumeSession, append(header, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	started := false
	_, err := Execute(context.Background(), req, func(e Event) { started = started || e.Type == "process.started" })
	if err == nil || started || !strings.Contains(err.Error(), "same canonical") {
		t.Fatalf("moved workdir resumed: %v started=%v", err, started)
	}
}

func TestCodexDevelopmentAndLeaderRetainDefaultModelAndIsolation(t *testing.T) {
	def, _ := DefaultRegistry().Resolve("codex")
	if def.CommitMode != "runner" {
		t.Fatal("builtin Codex must use trusted Runner snapshot")
	}
	for _, role := range []string{"", "leader", "review"} {
		args := launchArgs(def, Request{Role: role})
		model := false
		readOnly := false
		isolated := false
		for i, arg := range args {
			if arg == "--ignore-user-config" {
				isolated = true
			}
			if arg == "--model" && i+1 < len(args) && args[i+1] == "gpt-6.1-sol" {
				model = true
			}
			if arg == "--sandbox" && i+1 < len(args) && args[i+1] == "read-only" {
				readOnly = true
			}
		}
		if !isolated || !model || role == "leader" && !readOnly {
			t.Fatalf("unauthorized model or writable Leader: %v", args)
		}
	}
}

func TestLeaderEnvironmentDoesNotForwardOwnerOrPublicationAuthority(t *testing.T) {
	blocked := []string{"OWNER_TOKEN", "GH_TOKEN", "GITHUB_TOKEN", "GITLAB_TOKEN", "COORDICA_OWNER_TOKEN", "COORDICA_NODE_TOKEN", "SSH_AUTH_SOCK", "SSH_AGENT_PID", "OPENAI_BASE_URL", "OPENAI_MODEL", "CODEX_MODEL", "CUSTOM_PROXY"}
	for _, key := range blocked {
		t.Setenv(key, "fixture-"+key)
		t.Setenv(strings.ToLower(key), "fixture-lower-"+key)
	}
	t.Setenv("OPENAI_API_KEY", "fixture-model")
	for _, casing := range []string{"upper", "lower"} {
		t.Run(casing, func(t *testing.T) {
			proxies := map[string]string{"HTTPS_PROXY": "http://fixture-user:fixture-password@proxy.invalid:8080", "HTTP_PROXY": "http://proxy.invalid:8080", "ALL_PROXY": "socks5://proxy.invalid:1080", "NO_PROXY": "localhost,127.0.0.1,::1"}
			for key, value := range proxies {
				if casing == "lower" {
					key = strings.ToLower(key)
				}
				t.Setenv(key, value)
			}
			env := map[string]string{}
			for _, entry := range executionEnv(Request{Role: "leader"}) {
				key, value, _ := strings.Cut(entry, "=")
				env[key] = value
			}
			for _, key := range blocked {
				for _, name := range []string{key, strings.ToLower(key)} {
					if _, ok := env[name]; ok {
						t.Fatalf("Leader inherited excluded environment variable %s", name)
					}
				}
			}
			if env["OPENAI_API_KEY"] != "fixture-model" {
				t.Fatal("official model authentication missing")
			}
			for key, value := range proxies {
				if casing == "lower" {
					key = strings.ToLower(key)
				}
				if env[key] != value {
					t.Fatalf("Leader did not preserve transport environment variable %s", key)
				}
			}
		})
	}
}
