package runner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"coordica/internal/agent"
	"coordica/internal/domain"
	"coordica/internal/gitops"
)

func TestWorkerScopedRunnerCommitMode(t *testing.T) {
	for _, test := range []struct {
		name, kind, status                         string
		outside, ignored, cancel, unquiet, builtin bool
	}{
		{name: "authorized snapshot", kind: "implement", status: "succeeded"},
		{name: "builtin Codex file-only snapshot", kind: "implement", status: "succeeded", builtin: true},
		{name: "outside scope", kind: "implement", status: "failed", outside: true},
		{name: "ignored input", kind: "implement", status: "failed", ignored: true},
		{name: "cancelled", kind: "implement", status: "stopped", cancel: true},
		{name: "live process", kind: "implement", status: "failed", unquiet: true},
		{name: "review never snapshots", kind: "review", status: "failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			git := gitops.New("git")
			source := filepath.Join(t.TempDir(), "source")
			if err := git.Init(ctx, source, false); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(source, "code.txt"), []byte("base"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(source, ".gitignore"), []byte("cache/\n"), 0600); err != nil {
				t.Fatal(err)
			}
			base, err := git.CommitAll(ctx, source, "base", gitops.Identity{Name: "Test", Email: "test@example.invalid"})
			if err != nil {
				t.Fatal(err)
			}
			input := filepath.Join(t.TempDir(), "input.bundle")
			if _, err := git.CreateSnapshotBundle(ctx, source, base, input); err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			var completion domain.CompleteRequest
			var artifact []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				switch {
				case strings.HasSuffix(req.URL.Path, "input.bundle"):
					http.ServeFile(w, req, input)
				case strings.HasSuffix(req.URL.Path, "artifacts"):
					body, err := io.ReadAll(req.Body)
					if err != nil {
						t.Error(err)
					}
					mu.Lock()
					artifact = body
					mu.Unlock()
					respond(w, artifactTestReceipt(body))
				case strings.HasSuffix(req.URL.Path, "complete"):
					var value domain.CompleteRequest
					if err := json.NewDecoder(req.Body).Decode(&value); err != nil {
						t.Error(err)
					}
					mu.Lock()
					completion = value
					mu.Unlock()
					respond(w, domain.Run{ID: "run_test", Generation: 7, Quiescent: value.Quiescent, State: value.Status})
				default:
					t.Error("unexpected request", req.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			custom := agent.Definition{ID: "file-only-acp", Executables: []string{"fixture-acp"}, Protocol: agent.ProtocolACP, Cancel: true, Review: true, CommitMode: "runner"}
			tools := []agent.Definition{custom}
			tool := "file-only-acp"
			if test.builtin {
				tools = nil
				tool = "codex"
			}
			r, err := New(Config{CenterURL: server.URL, DataDir: t.TempDir(), NodeID: "node_test", Token: "test-node-token", Tools: tools})
			if err != nil {
				t.Fatal(err)
			}
			r.OnEvent = func(Event) {}
			r.execute = func(ctx context.Context, request agent.Request, onEvent func(agent.Event)) (agent.Result, error) {
				if request.Role == "implement" && (!strings.Contains(request.Prompt, "without running Git or committing") || strings.Contains(request.Prompt, "Commit intended changes yourself")) {
					t.Error("runner commit policy missing from prompt")
				}
				if err := os.WriteFile(filepath.Join(request.WorkDir, "code.txt"), []byte("private change"), 0600); err != nil {
					return agent.Result{}, err
				}
				if test.outside {
					if err := os.WriteFile(filepath.Join(request.WorkDir, "outside.txt"), []byte("private outside"), 0600); err != nil {
						return agent.Result{}, err
					}
				}
				if test.ignored {
					if err := os.Mkdir(filepath.Join(request.WorkDir, "cache"), 0700); err != nil {
						return agent.Result{}, err
					}
					if err := os.WriteFile(filepath.Join(request.WorkDir, "cache", "private.txt"), []byte("ignored"), 0600); err != nil {
						return agent.Result{}, err
					}
				}
				if test.unquiet {
					onEvent(agent.Event{Type: "process.started", Data: json.RawMessage(`{"pid":999999}`)})
				}
				if test.cancel {
					cancel()
				}
				return agent.Result{ExitCode: 0, TreeStopped: !test.unquiet, OutputText: "fixture result"}, nil
			}
			now := time.Now()
			assignment := domain.Assignment{Run: domain.Run{ID: "run_test", NodeID: "node_test", Generation: 7, Kind: test.kind, Tool: tool, Base: base.String(), LeaseUntil: now.Add(time.Minute), Deadline: now.Add(time.Minute)}, Task: domain.Task{Title: "change", Description: "change code", Acceptance: "test", AllowedPaths: []string{"code.txt"}}, InputURL: "/api/v1/runs/run_test/input.bundle"}
			if test.kind == "review" {
				assignment.Candidate = &domain.Candidate{Main: base.String(), Commit: base.String()}
			}
			record := spool{Assignment: assignment, Key: "worker-scoped", StartedAt: now, WorkDir: filepath.Join(r.config.DataDir, "runs", "worker", "workspace")}
			if err := writePrivateJSON(r.spoolPath(assignment.Run.ID), record); err != nil {
				t.Fatal(err)
			}
			r.perform(ctx, record, responseClock{Server: now, Sent: now, Received: now})
			mu.Lock()
			value, material := completion, append([]byte(nil), artifact...)
			mu.Unlock()
			if value.Status != test.status {
				t.Fatalf("completion=%+v", value)
			}
			head, err := git.Head(context.Background(), record.WorkDir)
			if err != nil {
				t.Fatal(err)
			}
			if test.status == "succeeded" {
				if value.Head != head.String() || value.ArtifactID == "" || len(material) == 0 || head == base {
					t.Fatalf("snapshot artifact absent: %+v", value)
				}
				bundle := filepath.Join(t.TempDir(), "result.bundle")
				if err := os.WriteFile(bundle, material, 0600); err != nil {
					t.Fatal(err)
				}
				restored := filepath.Join(t.TempDir(), "restored")
				if err := git.CloneSnapshot(context.Background(), bundle, head, restored); err != nil {
					t.Fatal(err)
				}
				if body, err := os.ReadFile(filepath.Join(restored, "code.txt")); err != nil || string(body) != "private change" {
					t.Fatal("uploaded snapshot omitted actual change", err)
				}
			} else {
				if head != base || value.ArtifactID != "" || len(material) != 0 {
					t.Fatalf("failed run committed or uploaded: head=%s completion=%+v", head, value)
				}
				if body, err := os.ReadFile(filepath.Join(record.WorkDir, "code.txt")); err != nil || string(body) != "private change" {
					t.Fatal("private failed changes discarded", err)
				}
			}
		})
	}
}
