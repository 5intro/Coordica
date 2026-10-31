package runner

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"coordica/internal/agent"
	"coordica/internal/app"
	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/process"
	"coordica/internal/store"
)

// Build C through the real implementation handoff and integrator. Only model
// output is a fixture; review failure, completion and scheduling use production
// code, authenticated HTTP, persisted accounting and actual Git snapshots.
func newReviewRetryFixture(t *testing.T) (*intentFixture, *app.Integrator) {
	t.Helper()
	f := newIntentFixtureWithRemote(t, true)
	f.r.execute = func(ctx context.Context, req agent.Request, _ func(agent.Event)) (agent.Result, error) {
		if err := os.WriteFile(filepath.Join(req.WorkDir, "test/greeting.test.mjs"), []byte("// implemented regression\n"), 0600); err != nil {
			t.Fatal(err)
		}
		return agent.Result{TreeStopped: true,
			OutputText: `{"version":1,"status":"completed","requested_paths":[],"reason":"","summary":"Implementation finished"}`}, nil
	}
	f.perform(context.Background())
	w := &app.Integrator{Service: f.s, Git: gitops.New("git")}
	for i := 0; i < 2; i++ {
		if err := w.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	snap := f.snapshot(t)
	if len(snap.Candidates) != 1 || snap.Candidates[0].State != "reviewing" {
		t.Fatalf("real integration did not prepare C: tasks=%+v runs=%+v candidates=%+v", snap.Tasks, snap.Runs, snap.Candidates)
	}
	original := f.r.http.Transport
	f.r.http.Transport = intentTransport(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "input.bundle") {
			var run domain.Run
			for _, r := range f.snapshot(t).Runs {
				if strings.Contains(req.URL.Path, r.ID) {
					run = r
				}
			}
			path, err := w.InputBundle(req.Context(), run)
			if err != nil {
				return nil, err
			}
			file, err := os.Open(path)
			if err != nil {
				return nil, err
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: file, Request: req}, nil
		}
		return original.RoundTrip(req)
	})
	return f, w
}

func reviewRetryClaim(t *testing.T, f *intentFixture) *domain.Assignment {
	t.Helper()
	b, err := f.s.Claim(context.Background(), f.node, app.ID("claim_"), domain.ClaimRequest{
		ReviewContractVersion: domain.ReviewContractVersion, AssignmentContextVersion: domain.AssignmentContextVersion,
		Capabilities: []domain.Capability{{Tool: "codex", Available: true, Structured: true, Review: true, Cancel: true}},
	}, f.base.String())
	if err != nil {
		t.Fatal(err)
	}
	var a *domain.Assignment
	if err = json.Unmarshal(b, &a); err != nil {
		t.Fatal(err)
	}
	return a
}

func failReviewGitRead(t *testing.T, f *intentFixture, a *domain.Assignment, unknown bool) {
	t.Helper()
	if a == nil || a.Run.Kind != "review" {
		t.Fatalf("expected fixed review: %+v", a)
	}
	f.record = spool{Assignment: *a, Key: app.ID("complete_"), StartedAt: time.Now(), WorkDir: filepath.Join(f.r.config.DataDir, "runs", a.Run.ID, "workspace")}
	if err := writePrivateJSON(f.r.spoolPath(a.Run.ID), f.record); err != nil {
		t.Fatal(err)
	}
	f.r.readReviewInput = func(ctx context.Context, a domain.Assignment, dir string) (string, error) {
		// The clone has succeeded. Make the actual managed Git read fail before the
		// reviewer starts; do not fabricate CompleteRequest or its classification.
		previous := f.r.git
		f.r.git = gitops.New(filepath.Join(t.TempDir(), "missing-git"))
		defer func() { f.r.git = previous }()
		input, err := f.r.reviewInput(ctx, a, dir)
		if unknown {
			err = errors.Join(err, process.ErrTreeNotStopped)
		}
		return input, err
	}
	f.r.execute = func(context.Context, agent.Request, func(agent.Event)) (agent.Result, error) {
		t.Error("agent executed without full input")
		return agent.Result{}, io.ErrUnexpectedEOF
	}
	f.perform(context.Background())
}

func TestReviewGitReadFailureRetriesSameCandidate(t *testing.T) {
	f, _ := newReviewRetryFixture(t)
	original := f.snapshot(t).Candidates[0]
	first := reviewRetryClaim(t, f)
	failReviewGitRead(t, f, first, false)
	snap := f.snapshot(t)
	if snap.Candidates[0].State != "reviewing" || snap.Candidates[0].Review != nil || snap.Tasks[0].Repairs != 0 || snap.Tasks[0].Retries != 1 {
		t.Fatalf("infrastructure error became a candidate verdict/repair: %+v %+v", snap.Tasks[0], snap.Candidates[0])
	}
	second := reviewRetryClaim(t, f)
	if second == nil || second.Run.Kind != "review" || second.Run.CandidateID != original.ID ||
		second.Run.Base != original.Commit || second.Run.Generation != first.Run.Generation+1 {
		t.Fatalf("did not retry same C: %+v", second)
	}
}

func resumeReviewTask(t *testing.T, f *intentFixture) error {
	t.Helper()
	task := f.snapshot(t).Tasks[0]
	_, err := f.s.TaskAction(context.Background(), f.owner, app.ID("resume_"), task.ID, "resume", domain.VersionRequest{ExpectedVersion: task.Version})
	return err
}

func TestReviewGitReadFailureExhaustsAutomaticRetriesThenOwnerResumesSameC(t *testing.T) {
	f, _ := newReviewRetryFixture(t)
	original := f.snapshot(t).Candidates[0]
	for i := 0; i < 3; i++ {
		failReviewGitRead(t, f, reviewRetryClaim(t, f), false)
	}
	snap := f.snapshot(t)
	task := snap.Tasks[0]
	if task.HoldReason != "human_decision" || task.Retries != task.MaxRetries || task.Repairs != 0 || !reflect.DeepEqual(original, snap.Candidates[0]) {
		t.Fatalf("retry exhaustion changed C or repairs: %+v", snap.Tasks)
	}
	if a := reviewRetryClaim(t, f); a != nil {
		t.Fatal("automatic retry exceeded finite limit", a)
	}
	// Existing Owner resume is an explicit human decision, not another automatic
	// retry grant. Preserve that contract rather than adding a new budget gate.
	if err := resumeReviewTask(t, f); err != nil {
		t.Fatal(err)
	}
	next := reviewRetryClaim(t, f)
	if next == nil || next.Run.Kind != "review" || next.Run.CandidateID != original.ID || next.Run.Base != original.Commit ||
		next.Task.Repairs != 0 || next.Task.Retries != task.Retries {
		t.Fatalf("Owner resume redeveloped/replaced C: %+v", next)
	}
}

func TestReviewGitReadFailureRestartReplaysExactHandoffOnce(t *testing.T) {
	f, _ := newReviewRetryFixture(t)
	a := reviewRetryClaim(t, f)
	transport := f.r.http.Transport
	f.r.http.Transport = intentTransport(func(req *http.Request) (*http.Response, error) {
		response, err := transport.RoundTrip(req)
		if strings.HasSuffix(req.URL.Path, "/complete") && err == nil {
			response.Body.Close()
			return nil, io.ErrUnexpectedEOF
		}
		return response, err
	})
	failReviewGitRead(t, f, a, false)
	var pending spool
	if err := readJSON(f.r.spoolPath(a.Run.ID), &pending); err != nil {
		t.Fatal(err)
	}
	if pending.Request == nil || pending.Request.Status != "transient_failure" || pending.Request.Review != nil || !pending.Request.Quiescent {
		t.Fatal("incorrect persisted failure", pending.Request)
	}
	next := reviewRetryClaim(t, f)
	if next == nil || next.Run.Kind != "review" || next.Run.CandidateID != a.Run.CandidateID || next.Run.Generation <= a.Run.Generation {
		t.Fatal("failure lost fixed candidate or generation", next)
	}
	// Replay the old completion after a newer generation already owns the task.
	before := f.snapshot(t)
	restarted, err := New(f.r.config)
	if err != nil {
		t.Fatal(err)
	}
	restarted.http.Transport = transport
	restarted.OnEvent = func(Event) {}
	if err = restarted.recoverRecords(context.Background()); err != nil {
		t.Fatal(err)
	}
	after := f.snapshot(t)
	if !reflect.DeepEqual(before.Tasks, after.Tasks) || !reflect.DeepEqual(before.Candidates, after.Candidates) ||
		!reflect.DeepEqual(before.Runs, after.Runs) || !reflect.DeepEqual(before.Project, after.Project) ||
		!reflect.DeepEqual(before.PlanningDocument, after.PlanningDocument) {
		t.Fatal("replayed failure charged twice or changed durable state")
	}
	if _, err = os.Stat(restarted.spoolPath(a.Run.ID)); !os.IsNotExist(err) {
		t.Fatal("acknowledged handoff not retired", err)
	}
	if after.Tasks[0].CurrentRunID != next.Run.ID {
		t.Fatal("old replay displaced current review")
	}
}

func TestReviewGitReadFailureOwnerResumeFences(t *testing.T) {
	for _, scenario := range []string{"candidate authority changed", "goal revoked", "budget exhausted", "unknown process"} {
		t.Run(scenario, func(t *testing.T) {
			f, _ := newReviewRetryFixture(t)
			a := reviewRetryClaim(t, f)
			failReviewGitRead(t, f, a, scenario == "unknown process")
			ctx := context.Background()
			snap := f.snapshot(t)
			switch scenario {
			case "candidate authority changed":
				c := snap.Candidates[0]
				c.AuthorizationVersion++
				if err := f.s.DB.Update(ctx, func(tx *store.Tx) error { return tx.Put("candidate", c.ID, c) }); err != nil {
					t.Fatal(err)
				}
			case "goal revoked":
				d := *snap.PlanningDocument
				d.Objective += " revised"
				if _, err := f.s.SavePlanning(ctx, f.owner, "change-goal", domain.PlanningSaveRequest{ExpectedVersion: d.Version, Document: d}); err != nil {
					t.Fatal(err)
				}
			case "budget exhausted":
				task := snap.Tasks[0]
				if _, err := f.s.TaskBudget(ctx, f.owner, "exhaust-budget", task.ID, domain.BudgetRequest{
					ExpectedVersion: task.Version, BudgetSeconds: task.UsedSeconds,
					MaxRetries: task.MaxRetries, MaxRepairs: task.MaxRepairs, Reason: "fixture finite remaining budget",
				}); err != nil {
					t.Fatal(err)
				}
			}
			err := resumeReviewTask(t, f)
			if scenario != "budget exhausted" && err == nil {
				t.Fatal("unsafe Owner resume admitted")
			}
			if next := reviewRetryClaim(t, f); next != nil {
				t.Fatal("unsafe claim admitted", next)
			}
			after := f.snapshot(t)
			if after.Tasks[0].Repairs != 0 || after.Candidates[0].Review != nil {
				t.Fatal("fence created repair or verdict")
			}
			if scenario == "unknown process" {
				var kept spool
				if err := readJSON(f.r.spoolPath(a.Run.ID), &kept); err != nil || kept.Request == nil ||
					kept.Request.Quiescent || kept.Request.Status == "transient_failure" {
					t.Fatal("unknown execution became retryable", err, kept)
				}
			}
		})
	}
}

func TestReviewerVerdictsStillRejectOrRepair(t *testing.T) {
	for _, verdict := range []string{"blocked", "changes_requested"} {
		t.Run(verdict, func(t *testing.T) {
			f, _ := newReviewRetryFixture(t)
			a := reviewRetryClaim(t, f)
			f.record = spool{Assignment: *a, Key: app.ID("complete_"), StartedAt: time.Now(), WorkDir: filepath.Join(f.r.config.DataDir, "runs", a.Run.ID, "workspace")}
			if err := writePrivateJSON(f.r.spoolPath(a.Run.ID), f.record); err != nil {
				t.Fatal(err)
			}
			f.r.execute = func(context.Context, agent.Request, func(agent.Event)) (agent.Result, error) {
				raw, err := json.Marshal(domain.Review{
					Candidate: a.Run.Base, Verdict: verdict, Summary: "Independent review fixture",
					Findings: []string{}, RulesDigest: a.Run.ReviewRulesDigest, Resolutions: []domain.ReviewResolution{},
					Issues: []domain.ReviewFinding{{Key: "review_fixture", Severity: "P2", Title: "Requires a code change", Path: "test/greeting.test.mjs", Trigger: "run greeting", Evidence: "regression output differs", Impact: "incorrect greeting"}},
				})
				return agent.Result{TreeStopped: true, OutputText: string(raw)}, err
			}
			f.perform(context.Background())
			snap := f.snapshot(t)
			c, task := snap.Candidates[0], snap.Tasks[0]
			if c.State != "rejected" || c.Review == nil || c.Review.Verdict != verdict || task.Retries != 0 {
				t.Fatal("valid reviewer verdict became platform retry", c, task)
			}
			if verdict == "blocked" && (task.HoldReason != "human_decision" || task.Repairs != 0) {
				t.Fatal("reviewer blocked no longer requires Owner", task)
			}
			if verdict == "changes_requested" && (task.Repairs != 1 || task.State != "ready" || task.CurrentCandidateID != "") {
				t.Fatal("reviewer change request no longer repairs", task)
			}
		})
	}
}
