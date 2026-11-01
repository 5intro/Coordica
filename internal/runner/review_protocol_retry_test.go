package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"coordica/internal/agent"
	"coordica/internal/app"
	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/process"
)

func performProtocolReview(t *testing.T, f *intentFixture, a *domain.Assignment,
	execute func(context.Context, agent.Request, func(agent.Event)) (agent.Result, error),
) {
	t.Helper()
	if a == nil || a.Run.Kind != "review" {
		t.Fatal("missing review assignment", a)
	}
	f.record = spool{Assignment: *a, Key: app.ID("complete_"), StartedAt: time.Now(),
		WorkDir: filepath.Join(f.r.config.DataDir, "runs", a.Run.ID, "workspace")}
	if err := writePrivateJSON(f.r.spoolPath(a.Run.ID), f.record); err != nil {
		t.Fatal(err)
	}
	f.r.execute = execute
	f.perform(context.Background())
}

func legacyProtocolOutput(a *domain.Assignment) string {
	b, _ := json.Marshal(domain.Review{Candidate: a.Run.Base, Verdict: "approve",
		Summary: "0 findings; source inspected", Findings: []string{"Positive scope observation"}})
	return string(b)
}

func TestReviewInvalidProtocolRetriesFixedCandidateWithoutRepairOrApproval(t *testing.T) {
	for _, kind := range []string{"legacy structured fields", "invalid JSON", "native protocol failure"} {
		t.Run(kind, func(t *testing.T) {
			f, _ := newReviewRetryFixture(t)
			original := f.snapshot(t).Candidates[0]
			first := reviewRetryClaim(t, f)
			performProtocolReview(t, f, first, func(context.Context, agent.Request, func(agent.Event)) (agent.Result, error) {
				result := agent.Result{TreeStopped: true, ExitCode: 0, SessionID: "review_protocol", OutputText: legacyProtocolOutput(first)}
				if kind == "invalid JSON" {
					result.OutputText = "not JSON"
				}
				if kind == "native protocol failure" {
					return result, agent.ErrProtocol
				}
				return result, nil
			})
			before := f.snapshot(t)
			if !reflect.DeepEqual(original, before.Candidates[0]) || before.Tasks[0].Retries != 1 ||
				before.Tasks[0].Repairs != 0 || before.Tasks[0].HoldReason != "" {
				t.Fatal("invalid output became a verdict, code repair or human hold", before.Tasks, before.Candidates)
			}
			next := reviewRetryClaim(t, f)
			if next == nil || next.Run.Base != original.Commit || next.Run.CandidateID != original.ID ||
				next.Run.Generation <= first.Run.Generation {
				t.Fatal("retry did not retain fixed C", next)
			}
			before = f.snapshot(t)
			_, err := f.s.Complete(context.Background(), f.node, "late-old-output", first.Run.ID,
				domain.CompleteRequest{Generation: first.Run.Generation, Status: "succeeded", Quiescent: true,
					Review: &domain.Review{Candidate: first.Run.Base, Verdict: "approve", Summary: "late approval",
						Findings: []string{}, RulesDigest: first.Run.ReviewRulesDigest,
						Issues: []domain.ReviewFinding{}, Resolutions: []domain.ReviewResolution{}}})
			if err != nil {
				t.Fatal(err)
			}
			after := f.snapshot(t)
			if !reflect.DeepEqual(before.Tasks, after.Tasks) || !reflect.DeepEqual(before.Candidates, after.Candidates) ||
				!reflect.DeepEqual(before.Runs, after.Runs) {
				t.Fatal("late result replaced current review")
			}
		})
	}
}

func TestReviewInvalidProtocolExhaustsFiniteRetriesOnSameCandidate(t *testing.T) {
	f, _ := newReviewRetryFixture(t)
	original := f.snapshot(t).Candidates[0]
	limit := f.snapshot(t).Tasks[0].MaxRetries
	for attempt := 0; attempt <= limit; attempt++ {
		a := reviewRetryClaim(t, f)
		performProtocolReview(t, f, a, func(context.Context, agent.Request, func(agent.Event)) (agent.Result, error) {
			return agent.Result{TreeStopped: true, SessionID: "malformed_review", OutputText: legacyProtocolOutput(a)}, nil
		})
	}
	after := f.snapshot(t)
	if after.Tasks[0].Retries != limit || after.Tasks[0].Repairs != 0 || after.Tasks[0].HoldReason != "human_decision" ||
		!reflect.DeepEqual(original, after.Candidates[0]) {
		t.Fatal("retry limit or fixed candidate changed", after.Tasks, after.Candidates)
	}
	if next := reviewRetryClaim(t, f); next != nil {
		t.Fatal("unbounded protocol retry", next)
	}
}

func TestReviewProtocolRetryNeedsStoppedUnchangedWorkspace(t *testing.T) {
	for _, kind := range []string{"unknown process", "dirty workspace", "changed candidate"} {
		t.Run(kind, func(t *testing.T) {
			f, _ := newReviewRetryFixture(t)
			original := f.snapshot(t).Candidates[0]
			a := reviewRetryClaim(t, f)
			performProtocolReview(t, f, a, func(ctx context.Context, req agent.Request, _ func(agent.Event)) (agent.Result, error) {
				result := agent.Result{TreeStopped: kind != "unknown process", SessionID: "unsafe_review"}
				if kind == "unknown process" {
					return result, errors.Join(agent.ErrProtocol, process.ErrTreeNotStopped)
				}
				if err := os.WriteFile(filepath.Join(req.WorkDir, "test/greeting.test.mjs"), []byte("// unexpected reviewer edit\n"), 0600); err != nil {
					t.Fatal(err)
				}
				if kind == "changed candidate" {
					if _, err := f.r.git.CommitAll(ctx, req.WorkDir, "unexpected review commit", gitops.Identity{Name: "Fixture", Email: "fixture@example.invalid"}); err != nil {
						t.Fatal(err)
					}
				}
				return result, agent.ErrProtocol
			})
			after := f.snapshot(t)
			if after.Tasks[0].Retries != 0 || after.Tasks[0].Repairs != 0 || !reflect.DeepEqual(original, after.Candidates[0]) {
				t.Fatal("unverified execution became retryable", after.Tasks, after.Candidates)
			}
			if next := reviewRetryClaim(t, f); next != nil {
				t.Fatal("unsafe review restarted", next)
			}
		})
	}
}

func TestReviewProtocolFailureCannotInferStopOrOverrideCancellation(t *testing.T) {
	for _, kind := range []string{"no stop receipt", "cancelled", "unknown descendant"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := agent.Result{TreeStopped: kind != "no stop receipt"}
			cause := agent.ErrProtocol
			if kind == "cancelled" {
				cancel()
			}
			if kind == "unknown descendant" {
				cause = errors.Join(cause, process.ErrTreeNotStopped)
			}
			completion := domain.CompleteRequest{Status: "failed", Quiescent: true}
			// A nil Git adapter proves these cases cannot reach retry verification.
			err := (&Runner{}).reviewProtocolFailure(ctx, t.TempDir(), gitops.OID("fixed"), result, &completion, cause)
			if completion.Status != "failed" || !errors.Is(err, agent.ErrProtocol) {
				t.Fatal("uncertain stop/cancellation became retryable", completion, err)
			}
			if kind == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost", err)
			}
		})
	}
}

func TestReviewProtocolRetryCannotReviveRevokedGoal(t *testing.T) {
	f, _ := newReviewRetryFixture(t)
	a := reviewRetryClaim(t, f)
	performProtocolReview(t, f, a, func(ctx context.Context, _ agent.Request, _ func(agent.Event)) (agent.Result, error) {
		document := *f.snapshot(t).PlanningDocument
		document.Objective += " changed by Owner"
		if _, err := f.s.SavePlanning(ctx, f.owner, "revoke-review-goal", domain.PlanningSaveRequest{
			ExpectedVersion: document.Version, Document: document,
		}); err != nil {
			t.Fatal(err)
		}
		return agent.Result{TreeStopped: true, SessionID: "stale_review", OutputText: legacyProtocolOutput(a)}, nil
	})
	after := f.snapshot(t)
	if after.Tasks[0].Retries != 0 || after.Tasks[0].Repairs != 0 || after.Candidates[0].Review != nil {
		t.Fatal("old authority minted a retry or approval", after.Tasks, after.Candidates)
	}
	if next := reviewRetryClaim(t, f); next != nil {
		t.Fatal("revoked review restarted", next)
	}
}
