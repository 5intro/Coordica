package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestACPCatalogReusesOwnedSessionAcrossConnections(t *testing.T) {
	for _, tool := range []string{"dsh", "kimi", "cursor"} {
		t.Run(tool, func(t *testing.T) {
			t.Setenv("AGENT_TEST_MODEL_PROTOCOL", "acp")
			req := fixtureRequest(t, tool, "unused")
			log := filepath.Join(t.TempDir(), "requests")
			t.Setenv("AGENT_TEST_SESSION_LOG", log)
			def, _ := DefaultRegistry().Resolve(tool)
			for i := 0; i < 3; i++ {
				// Bypass the in-memory TTL: separate connections (including a
				// fresh Registry) must resume the same persistent probe.
				def, _ = DefaultRegistry().Resolve(tool)
				models, err := readToolModels(context.Background(), def, req.Executable)
				if err != nil || len(models) != 2 {
					t.Fatal(models, err)
				}
			}
			data, err := os.ReadFile(log)
			if err != nil || strings.Count(string(data), "session/new\n") != 1 || strings.Count(string(data), def.ResumeMethod+"\n") != 2 || strings.Contains(string(data), "session/prompt") {
				t.Fatal("catalog polling leaked sessions or sent a prompt", string(data), err)
			}
		})
	}
}

func TestACPCatalogRefusesToCreateWithoutResumeAndAfterLostNewReply(t *testing.T) {
	for _, mode := range []string{"missing", "lost"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("AGENT_TEST_MODEL_PROTOCOL", "acp")
			req := fixtureRequest(t, "dsh", "unused")
			log := filepath.Join(t.TempDir(), "requests")
			t.Setenv("AGENT_TEST_SESSION_LOG", log)
			if mode == "missing" {
				t.Setenv("AGENT_TEST_RESUME", "missing")
			} else {
				t.Setenv("AGENT_TEST_NEW_REPLY", "lost")
			}
			def, _ := DefaultRegistry().Resolve("dsh")
			for i := 0; i < 2; i++ {
				if models, err := readToolModels(context.Background(), def, req.Executable); err == nil || len(models) != 0 {
					t.Fatal("uncertain session created a successful catalog", err, models)
				}
			}
			data, err := os.ReadFile(log)
			want := 0
			if mode == "lost" {
				want = 1
			}
			if err != nil || strings.Count(string(data), "session/new\n") != want {
				t.Fatal("uncertain discovery kept creating sessions", err, string(data))
			}
		})
	}
}

func TestACPCatalogResumeFailureDoesNotCreateReplacement(t *testing.T) {
	t.Setenv("AGENT_TEST_MODEL_PROTOCOL", "acp")
	req := fixtureRequest(t, "dsh", "unused")
	log := filepath.Join(t.TempDir(), "requests")
	t.Setenv("AGENT_TEST_SESSION_LOG", log)
	def, _ := DefaultRegistry().Resolve("dsh")
	if _, err := readToolModels(context.Background(), def, req.Executable); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_TEST_RESUME", "reject")
	if _, err := readToolModels(context.Background(), def, req.Executable); err == nil {
		t.Fatal("failed resume was silently replaced")
	}
	data, _ := os.ReadFile(log)
	if strings.Count(string(data), "session/new\n") != 1 {
		t.Fatal("rejected resume created another persistent session", string(data))
	}
}

func TestCatalogSessionLockAndCorruptionFailClosed(t *testing.T) {
	req := fixtureRequest(t, "dsh", "unused")
	def, _ := DefaultRegistry().Resolve("dsh")
	state, unlock, err := lockCatalogSession(def, req.Executable)
	if err != nil {
		t.Fatal(err)
	}
	if _, release, err := lockCatalogSession(def, req.Executable); err == nil {
		release()
		t.Fatal("two probes acquired the same persistent session")
	}
	unlock()
	if err := os.WriteFile(state.path, []byte(`{"version":1,"identity":"wrong","session_id":"a-user-session","pending":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, release, err := lockCatalogSession(def, req.Executable); err == nil {
		release()
		t.Fatal("corrupt identity adopted an arbitrary session")
	}
}

func TestACPCatalogBoundsNonProtocolDiagnostics(t *testing.T) {
	for _, noise := range []string{"line", "flood", "malformed"} {
		t.Run(noise, func(t *testing.T) {
			t.Setenv("AGENT_TEST_MODEL_PROTOCOL", "acp")
			t.Setenv("AGENT_TEST_METADATA_NOISE", noise)
			req := fixtureRequest(t, "dsh", "unused")
			def, _ := DefaultRegistry().Resolve("dsh")
			models, err := readToolModels(context.Background(), def, req.Executable)
			if noise == "line" {
				if err != nil || len(models) != 2 {
					t.Fatal("plain log hid structured catalog", models, err)
				}
			} else if err == nil || len(models) != 0 {
				t.Fatal("unbounded noise or malformed protocol accepted", models, err)
			}
		})
	}
}

func TestACPCatalogAndExecutionPreserveOpaqueModelValues(t *testing.T) {
	t.Setenv("AGENT_TEST_MODEL_PROTOCOL", "acp")
	t.Setenv("AGENT_TEST_OPAQUE_MODEL", "1")
	req := fixtureRequest(t, "dsh", "unused")
	def, _ := DefaultRegistry().Resolve("dsh")
	models, err := readToolModels(context.Background(), def, req.Executable)
	if err != nil || len(models) != 2 || models[0].ID != `["provider","reported-model"]` {
		t.Fatal("opaque catalog value changed", models, err)
	}
	req.Model = models[0].ID
	req.ReasoningEffort = "max"
	result, err := Execute(context.Background(), req, nil)
	if err != nil || !result.TreeStopped {
		t.Fatal("reported opaque model could not be selected", result, err)
	}
}

func TestACPCatalogExplicitCreationRejectionCanRetryButUncertainErrorsCannot(t *testing.T) {
	for _, mode := range []string{"reject", "wrong-error-id", "malformed-error", "mixed-result-error", "lost"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("AGENT_TEST_MODEL_PROTOCOL", "acp")
			req := fixtureRequest(t, "dsh", "unused")
			log := filepath.Join(t.TempDir(), "requests")
			t.Setenv("AGENT_TEST_SESSION_LOG", log)
			t.Setenv("AGENT_TEST_NEW_REPLY", mode)
			def, _ := DefaultRegistry().Resolve("dsh")
			if _, err := readToolModels(context.Background(), def, req.Executable); err == nil {
				t.Fatal("rejected creation succeeded")
			}
			t.Setenv("AGENT_TEST_NEW_REPLY", "")
			models, err := readToolModels(context.Background(), def, req.Executable)
			if mode == "reject" {
				if err != nil || len(models) != 2 {
					t.Fatal("explicit rejection permanently disabled catalog", models, err)
				}
				if _, err := readToolModels(context.Background(), def, req.Executable); err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("uncertain response permitted a replacement session")
			}
			data, _ := os.ReadFile(log)
			want := 1
			if mode == "reject" {
				want = 2
			}
			if strings.Count(string(data), "session/new\n") != want {
				t.Fatal("wrong creation retry count", mode, string(data))
			}
		})
	}
}
