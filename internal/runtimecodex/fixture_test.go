package runtimecodex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"coordica/internal/agent/appserver"
	"coordica/internal/runtime"
	"coordica/internal/store"
)

// Hand-written JSON-RPC child, never a native recording or real harness.
func TestMain(m *testing.M) {
	if os.Getenv("COORDICA_COMPOSE_FIXTURE") != "1" {
		os.Exit(m.Run())
	}
	if len(os.Args) != 2 || os.Args[1] != "app-server" {
		os.Exit(2)
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024), 256<<10)
	for scanner.Scan() {
		var frame struct {
			ID     json.RawMessage
			Method string
			Params json.RawMessage
		}
		if json.Unmarshal(scanner.Bytes(), &frame) != nil {
			os.Exit(3)
		}
		f, err := os.OpenFile(os.Getenv("COORDICA_COMPOSE_JOURNAL"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			os.Exit(4)
		}
		_, err = f.Write(append(append([]byte(nil), scanner.Bytes()...), '\n'))
		f.Close()
		if err != nil {
			os.Exit(5)
		}
		if frame.Method == "initialized" {
			continue
		}
		reply := func(value any) {
			raw, _ := json.Marshal(map[string]any{"id": frame.ID, "result": value})
			fmt.Println(string(raw))
		}
		fail := func() {
			raw, _ := json.Marshal(map[string]any{"id": frame.ID, "error": map[string]any{"code": -32001, "message": "fixture rejection"}})
			fmt.Println(string(raw))
		}
		var params struct {
			ThreadID string `json:"threadId"`
			Model    string `json:"model"`
			Cwd      string `json:"cwd"`
		}
		if json.Unmarshal(frame.Params, &params) != nil {
			os.Exit(6)
		}
		scenario := os.Getenv("COORDICA_COMPOSE_SCENARIO")
		switch frame.Method {
		case "initialize":
			if scenario == "initialize-error-pause" {
				fixturePrivateError(frame.ID)
				continue
			}
			if scenario == "initialize-rejected" {
				fail()
				continue
			}
			reply(map[string]string{"userAgent": "offline-composition-fixture", "platformFamily": "fixture", "platformOs": "fixture"})
		case "thread/start", "thread/resume":
			if frame.Method == "thread/resume" && scenario == "resume-error-pause" {
				fixturePrivateError(frame.ID)
				continue
			}
			if frame.Method == "thread/resume" && params.ThreadID != "native-thread" {
				fail()
				continue
			}
			if frame.Method == "thread/resume" && scenario == "resume-disconnect" {
				os.Exit(0)
			}
			if frame.Method == "thread/resume" && scenario == "resume-rejected" {
				fail()
				continue
			}
			id, model := "native-thread", params.Model
			if frame.Method == "thread/resume" && scenario == "resume-model-mismatch" {
				model = "different-model"
			}
			if frame.Method == "thread/resume" && scenario == "resume-wrong-id" {
				id = "wrong-native-thread"
			}
			reply(map[string]any{"thread": map[string]any{"id": id, "turns": []any{}}, "model": model, "modelProvider": "fixture",
				"cwd": params.Cwd, "approvalPolicy": "on-request", "sandbox": map[string]string{"type": "readOnly"}})
		case "thread/read":
			if scenario == "read-disconnect" {
				os.Exit(0)
			}
			if scenario == "read-rejected" {
				fail()
				continue
			}
			if scenario == "read-pause" || scenario == "read-error-pause" {
				deadline := time.Now().Add(5 * time.Second)
				for {
					if _, err := os.Stat(os.Getenv("COORDICA_COMPOSE_CONTINUE")); err == nil {
						break
					}
					if time.Now().After(deadline) {
						os.Exit(7)
					}
					time.Sleep(5 * time.Millisecond)
				}
			}
			if scenario == "read-error-pause" {
				raw, _ := json.Marshal(map[string]any{"id": frame.ID, "error": map[string]any{"code": -32001,
					"message": "private-history-sentinel", "data": map[string]string{"private": "private-history-sentinel"}}})
				fmt.Println(string(raw))
				continue
			}
			reply(map[string]any{"thread": map[string]any{"id": "native-thread", "turns": []any{}, "private": "private-history-sentinel"}})
		default:
			fail()
		}
	}
	os.Exit(0)
}

func fixturePrivateError(id json.RawMessage) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(os.Getenv("COORDICA_COMPOSE_CONTINUE")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			os.Exit(8)
		}
		time.Sleep(5 * time.Millisecond)
	}
	raw, _ := json.Marshal(map[string]any{"id": id, "error": map[string]any{"code": -32001, "message": "private-control-error-sentinel",
		"data": map[string]string{"private": "private-control-error-sentinel"}}})
	fmt.Println(string(raw))
}

type fixture struct {
	db                     *store.Store
	ledger                 *runtime.Ledger
	manager                *runtime.Manager
	grant                  runtime.GrantConfig
	cred                   runtime.Credential
	producer               *runtime.BindingProducer
	port                   *Port
	binding                runtime.Binding
	path, journal, proceed string
}

func setup(t *testing.T, scenario string) *fixture {
	t.Helper()
	dir := t.TempDir()
	f := &fixture{path: filepath.Join(dir, "ledger.db"), journal: filepath.Join(dir, "journal"), proceed: filepath.Join(dir, "continue")}
	var err error
	f.db, err = store.Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.db.Close() })
	f.ledger, f.manager = runtime.New(f.db), runtime.NewManager(f.db)
	f.grant = runtime.GrantConfig{Grant: runtime.Grant{ID: "principal", ApplicationID: "app", Enabled: true,
		Actions: []runtime.Action{runtime.Submit, runtime.Read, runtime.Register, runtime.Cancel, runtime.SessionControl},
		Targets: []runtime.TargetScope{{InstanceID: "instance", NodeID: "node"}}}, Token: strings.Repeat("offline-test-secret-", 3)}
	if _, err = f.manager.ConfigureGrant(t.Context(), f.grant, 0); err != nil {
		t.Fatal(err)
	}
	if _, err = f.manager.ConfigureInstance(t.Context(), runtime.Instance{ID: "instance", ApplicationID: "app", NodeID: "node", Harness: "codex",
		ConfigRef: "host-config", Support: "unknown", Enabled: true}, 0); err != nil {
		t.Fatal(err)
	}
	f.cred = runtime.Credential{PrincipalID: f.grant.ID, Token: f.grant.Token}
	target := runtime.Target{InstanceID: "instance", InstanceVersion: 1, NodeID: "node", Harness: "codex", Model: "fixture-model", Effort: "ultra", WorkspaceRef: "ordinary-workspace"}
	config := runtime.BindingConfig{Target: target, InstanceConfigRef: "host-config", ProfileRef: "host-profile", WorkspaceFingerprint: "host-workspace",
		ProtocolVersion: "pinned-fixture", ApprovalPolicy: "on-request", Sandbox: "read-only"}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	env := []string{"COORDICA_COMPOSE_FIXTURE=1", "COORDICA_COMPOSE_JOURNAL=" + f.journal, "COORDICA_COMPOSE_SCENARIO=" + scenario,
		"COORDICA_COMPOSE_CONTINUE=" + f.proceed, "GOMAXPROCS=1", "GORACE=atexit_sleep_ms=0"}
	for _, name := range []string{"PATH", "SystemRoot", "TMPDIR", "TMP", "TEMP"} {
		if value := os.Getenv(name); value != "" {
			env = append(env, name+"="+value)
		}
	}
	f.port, err = New(t.Context(), config, appserver.Config{Executable: executable, Directory: dir, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		f.port.mu.Lock()
		defer f.port.mu.Unlock()
		for _, l := range f.port.slots {
			l.mu.Lock()
			if l.peer != nil {
				result, err := l.peer.Close()
				if !result.TreeStopped {
					t.Errorf("fixture still alive: %+v %v", result, err)
				}
			}
			l.mu.Unlock()
		}
	})
	f.producer, err = runtime.NewBindingProducer(f.db, config, f.port)
	if err != nil {
		t.Fatal(err)
	}
	task, err := f.ledger.SubmitTask(t.Context(), f.cred, runtime.SubmitTaskRequest{Key: "submit", Title: "programmatic", Context: "TASK-SECRET-NEVER-SEND"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := f.ledger.RegisterRun(t.Context(), f.cred, runtime.RegisterRunRequest{Key: "run", TaskID: task.Task.ID, ExpectedVersion: 1, ExpectedContextVersion: 1, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	f.binding, err = f.producer.PrepareBinding(t.Context(), f.cred, runtime.PrepareBindingRequest{Key: "prepare", TaskID: task.Task.ID, RunID: run.Run.ID, RunGeneration: 1, ExpectedVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func produce(t *testing.T, f *fixture) runtime.Binding {
	t.Helper()
	b, err := f.producer.Produce(t.Context(), f.cred, f.binding.ID)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func release(t *testing.T, f *fixture, b runtime.Binding) runtime.Binding {
	t.Helper()
	b, err := f.producer.Release(t.Context(), f.cred, runtime.BindingRequest{Key: "release", BindingID: b.ID, ExpectedVersion: b.Version, Generation: b.Generation})
	if err != nil {
		t.Fatal(err)
	}
	return b
}
