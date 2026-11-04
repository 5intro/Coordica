package runner

import (
	"context"
	"coordica/internal/agent"
	"coordica/internal/domain"
	"coordica/internal/gitops"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Desired behavior, intentionally red on 68fa9ee. No real model is invoked.
func TestArtifactTransientUploadKeepsVerifiedResult(t *testing.T) {
	for _, mode := range []string{"503-before-accept", "ack-lost-after-accept"} {
		t.Run(mode, func(t *testing.T) {
			base, input := artifactRecoverySource(t)
			attempts, executions := 0, 0
			var material []byte
			var completion domain.CompleteRequest
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				switch {
				case req.Method == http.MethodPost && req.URL.Path == "/api/v1/runs/run_test/heartbeat":
					var heartbeat domain.HeartbeatRequest
					if err := json.NewDecoder(req.Body).Decode(&heartbeat); err != nil || heartbeat.Generation != 7 {
						t.Errorf("invalid artifact fixture heartbeat: %+v, %v", heartbeat, err)
						w.WriteHeader(http.StatusConflict)
						return
					}
					respond(w, domain.HeartbeatResponse{LeaseUntil: time.Now().Add(time.Minute)})
				case strings.HasSuffix(req.URL.Path, "input.bundle"):
					http.ServeFile(w, req, input)
				case strings.HasSuffix(req.URL.Path, "artifacts"):
					body, e := io.ReadAll(req.Body)
					if e != nil {
						t.Error(e)
						return
					}
					attempts++
					if material != nil && string(material) != string(body) {
						t.Error("retry changed fixed bundle")
					}
					material = body
					if attempts == 1 {
						if mode == "503-before-accept" {
							w.WriteHeader(503)
							return
						}
						// Server consumed/accepted the complete body, but the receipt is truncated.
						w.Header().Set("Content-Type", "application/json")
						io.WriteString(w, `{"id":`)
						return
					}
					digest := sha256.Sum256(body)
					sum := hex.EncodeToString(digest[:])
					respond(w, domain.Artifact{ID: "run_test_" + sum, RunID: "run_test", SHA256: sum, Size: int64(len(body))})
				case strings.HasSuffix(req.URL.Path, "complete"):
					if e := json.NewDecoder(req.Body).Decode(&completion); e != nil {
						t.Error(e)
					}
					respond(w, domain.Run{ID: "run_test", Generation: 7, Quiescent: completion.Quiescent, State: completion.Status})
				default:
					t.Error("unexpected request", req.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			// Exercise the real permission heartbeat without depending on slow Git
			// preparation reaching its ten-second interval on the current host.
			client := testRunner(t, server)
			var heartbeat domain.HeartbeatResponse
			clock, err := client.json(t.Context(), "POST", "/api/v1/runs/run_test/heartbeat", "",
				domain.HeartbeatRequest{Generation: 7, ElapsedSeconds: 11}, &heartbeat)
			if err != nil || heartbeat.Cancel || !heartbeat.LeaseUntil.After(clock.Received) {
				t.Fatalf("artifact recovery fixture rejected a valid heartbeat: %+v, %v", heartbeat, err)
			}
			if attempts != 0 || material != nil || completion.Status != "" || completion.Quiescent ||
				completion.Head != "" || completion.ArtifactID != "" {
				t.Fatal("heartbeat changed artifact or completion evidence")
			}
			executions = performArtifactRecovery(t, server, base)
			if completion.Status != "succeeded" || attempts != 2 || executions != 1 || completion.ArtifactID == "" || completion.Head == "" {
				t.Fatalf("verified result not delivered: uploads=%d executions=%d completion=%+v", attempts, executions, completion)
			}
		})
	}
}

func artifactRecoverySource(t *testing.T) (gitops.OID, string) {
	t.Helper()
	ctx := context.Background()
	git := gitops.New("git")
	source := filepath.Join(t.TempDir(), "source")
	if err := git.Init(ctx, source, false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "code.txt"), []byte("base"), 0600); err != nil {
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
	return base, input
}

func performArtifactRecovery(t *testing.T, server *httptest.Server, base gitops.OID) int {
	t.Helper()
	executions := 0
	definition := agent.Definition{
		ID: "artifact-fixture", Executables: []string{"fixture"}, Protocol: agent.ProtocolACP, Cancel: true, Review: true, CommitMode: "runner",
	}
	r, err := New(Config{CenterURL: server.URL, DataDir: t.TempDir(), NodeID: "node_test", Tools: []agent.Definition{definition}})
	if err != nil {
		t.Fatal(err)
	}
	r.OnEvent = func(Event) {}
	r.execute = func(_ context.Context, request agent.Request, _ func(agent.Event)) (agent.Result, error) {
		executions++
		err := os.WriteFile(filepath.Join(request.WorkDir, "code.txt"), []byte("verified change"), 0600)
		return agent.Result{ExitCode: 0, TreeStopped: true, OutputText: "fixture"}, err
	}
	now := time.Now()
	assignment := domain.Assignment{
		Run: domain.Run{
			ID: "run_test", NodeID: "node_test", Generation: 7, Kind: "implement", Tool: definition.ID, Base: base.String(),
			LeaseUntil: now.Add(time.Minute), Deadline: now.Add(time.Minute),
		},
		Task:     domain.Task{Title: "change", Description: "change", Acceptance: "test", AllowedPaths: []string{"code.txt"}},
		InputURL: "/api/v1/runs/run_test/input.bundle",
	}
	record := spool{Assignment: assignment, Key: "fixed-result", StartedAt: now, WorkDir: filepath.Join(r.config.DataDir, "runs", "worker", "workspace")}
	if err := writePrivateJSON(r.spoolPath(assignment.Run.ID), record); err != nil {
		t.Fatal(err)
	}
	r.perform(context.Background(), record, responseClock{Server: now, Sent: now, Received: now})
	return executions
}
