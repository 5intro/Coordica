package runner

import (
	"context"
	"encoding/json"
	"errors"
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
	"coordica/internal/process"
)

func TestReviewInputFailurePreservesUnknownGitTreeAndCancellation(t *testing.T) {
	git := gitops.New("git")
	source := filepath.Join(t.TempDir(), "source")
	if err := git.Init(context.Background(), source, false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "code.txt"), []byte("fixed candidate"), 0600); err != nil {
		t.Fatal(err)
	}
	base, err := git.CommitAll(context.Background(), source, "base", gitops.Identity{Name: "Test", Email: "test@example.invalid"})
	if err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(t.TempDir(), "input.bundle")
	if _, err := git.CreateSnapshotBundle(context.Background(), source, base, bundle); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, status    string
		schemaTool      string
		unquiet, cancel bool
	}{
		{name: "unknown tree", status: "failed", unquiet: true},
		{name: "cancelled unknown tree", status: "stopped", unquiet: true, cancel: true},
		{name: "ordinary unavailable diff", status: "succeeded"},
		{name: "cancelled ordinary diff", status: "stopped", cancel: true},
		{name: "native Claude review schema", status: "succeeded", schemaTool: "claude"},
		{name: "native Codex review schema", status: "succeeded", schemaTool: "codex"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var mu sync.Mutex
			var completion domain.CompleteRequest
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				switch {
				case strings.HasSuffix(req.URL.Path, "/input.bundle"):
					http.ServeFile(w, req, bundle)
				case strings.HasSuffix(req.URL.Path, "/complete"):
					var value domain.CompleteRequest
					if err := json.NewDecoder(req.Body).Decode(&value); err != nil {
						t.Error(err)
					}
					mu.Lock()
					completion = value
					mu.Unlock()
					respond(w, domain.Run{ID: "run_test", Generation: 7, State: value.Status, Quiescent: value.Quiescent})
				default:
					t.Errorf("unexpected request: %s", req.URL.Path)
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			defer server.Close()
			r := testRunner(t, server)
			r.OnEvent = func(Event) {}
			r.readReviewInput = func(ctx context.Context, assignment domain.Assignment, dir string) (string, error) {
				if test.schemaTool != "" {
					return "Complete fixed review fixture", nil
				}
				if test.cancel {
					cancel()
				}
				err := error(gitops.ErrGit)
				if test.unquiet {
					err = errors.Join(err, process.ErrTreeNotStopped)
				}
				return "", &gitops.Error{Op: "diff", Kind: err, Detail: "review Git command failed"}
			}
			r.execute = func(_ context.Context, request agent.Request, _ func(agent.Event)) (agent.Result, error) {
				if test.schemaTool != "" {
					raw, err := os.ReadFile(request.OutputSchemaPath)
					if err != nil || string(raw) != string(reviewOutputSchema(base.String())) || strings.Join(request.AllowedTools, ",") != "Read,Glob,Grep" || strings.Contains(request.Prompt, nativeIntentPrompt) {
						t.Fatal("review schema or read-only restrictions lost", err)
					}
					review, _ := json.Marshal(domain.Review{Candidate: base.String(), Verdict: "approve", Summary: "Fixture independent review", Findings: []string{}})
					return agent.Result{ExitCode: 0, TreeStopped: true, OutputText: string(review)}, nil
				}
				t.Error("review Agent ran without a complete fixed diff")
				return agent.Result{}, errors.New("unexpected execution")
			}
			now := time.Now()
			assignment := domain.Assignment{
				Run:       domain.Run{ID: "run_test", NodeID: "node_test", Generation: 7, Kind: "review", Tool: "claude", Base: base.String(), LeaseUntil: now.Add(time.Minute), Deadline: now.Add(time.Minute)},
				Task:      domain.Task{Title: "review", Description: "review candidate", Acceptance: "fixed diff", AllowedPaths: []string{"code.txt"}},
				Candidate: &domain.Candidate{Main: base.String(), Commit: base.String()},
				InputURL:  "/api/v1/runs/run_test/input.bundle",
			}
			if test.schemaTool != "" {
				assignment.Run.Tool = test.schemaTool
			}
			record := spool{Assignment: assignment, Key: "review-input-key", StartedAt: now, WorkDir: filepath.Join(r.config.DataDir, "runs", "review", "workspace")}
			if test.unquiet {
				// A previous Agent PID is not evidence for this unknown Git tree.
				record.PID = os.Getpid()
			}
			if err := writePrivateJSON(r.spoolPath(assignment.Run.ID), record); err != nil {
				t.Fatal(err)
			}
			r.perform(ctx, record, responseClock{Server: now, Sent: now, Received: now})
			mu.Lock()
			value := completion
			mu.Unlock()
			if value.Status != test.status || value.Quiescent == test.unquiet || value.Head != "" || value.ArtifactID != "" {
				t.Fatalf("review failure changed stop evidence: %+v", value)
			}
			if test.cancel && (!strings.Contains(value.Detail, context.Canceled.Error()) || !strings.Contains(value.Detail, "review Git command failed")) {
				t.Fatalf("cancellation discarded Git failure: %+v", value)
			}
			if test.unquiet {
				if value.Review != nil {
					t.Fatal("unknown tree became an accepted blocked review")
				}
				var kept spool
				if err := readJSON(r.spoolPath(assignment.Run.ID), &kept); err != nil || kept.PID != 0 || kept.Request == nil || kept.Request.Quiescent || !kept.Acknowledged {
					t.Fatalf("unknown Git tree evidence was discarded: record=%+v err=%v", kept, err)
				}
				if count, err := r.occupiedSlots(); err != nil || count != 1 {
					t.Fatalf("unknown Git execution freed local capacity: %d %v", count, err)
				}
			} else if test.schemaTool != "" {
				if value.Review == nil || value.Review.Verdict != "approve" || !value.Quiescent {
					t.Fatal("valid native review not delivered", value)
				}
			} else if !test.cancel && (value.Review == nil || value.Review.Verdict != "blocked") {
				t.Fatal("ordinary diff failure did not preserve blocked-review behavior")
			}
		})
	}
}
