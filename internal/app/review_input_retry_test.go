package app

import (
	"context"
	"reflect"
	"strconv"
	"sync"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func blockedReviewInputFixture(t *testing.T) (*itFixture, domain.Task, domain.Candidate, domain.Assignment) {
	t.Helper()
	f := newITFixture(t)
	f.implement(t, "src/feature.txt")
	c := f.candidate(t)
	review := f.claim(t, "review")
	_, err := f.s.Complete(context.Background(), f.node, "preflight-result", review.Run.ID, domain.CompleteRequest{
		Generation: review.Run.Generation, Status: "succeeded", Quiescent: true, ElapsedSeconds: 1,
		Detail: "review blocked before agent execution: complete review diff exceeds 1 MiB",
		Review: &domain.Review{Candidate: c.Commit, Verdict: "blocked", Summary: "Complete fixed review input is unavailable", Findings: []string{"input size"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	snap := f.snapshot(t)
	return f, snap.Tasks[0], snap.Candidates[0], review
}

func reviewInputRetryRequest(task domain.Task, c domain.Candidate) domain.ReviewInputRetryRequest {
	return domain.ReviewInputRetryRequest{ExpectedVersion: task.Version, ExpectedCandidateID: c.ID, ExpectedCandidateVersion: c.Version,
		ExpectedCommit: c.Commit, ReviewRunID: c.Review.RunID, Reason: "Fixed material delivery is repaired; retry the same code with an independent reviewer"}
}

func TestReviewInputRetryPreservesCodeAccountingHistoryAndRequiresNewReview(t *testing.T) {
	f, before, old, first := blockedReviewInputFixture(t)
	ctx := context.Background()
	in := reviewInputRetryRequest(before, old)
	raw, err := RetryReviewInput(ctx, f.s, f.owner, "retry-input", before.ID, in)
	after := itDecode[domain.Task](t, raw, err)
	expected := before
	expected.Version++
	expected.Retries++
	expected.HoldReason = "paused"
	if !reflect.DeepEqual(after, expected) {
		t.Fatal("retry changed source authority, accounting or repair limit", after, before)
	}
	want := old
	want.State, want.Detail, want.Review = "reviewing", "Owner requested bounded material retry", nil
	want.Version++
	if !reflect.DeepEqual(f.candidate(t), want) {
		t.Fatal("retry changed fixed candidate or discarded prechecks")
	}
	if err = f.s.DB.View(ctx, func(tx *store.Tx) error {
		var history reviewInputRetryReceipt
		if err := tx.Get("review_input_retry_history", old.ID+"_"+strconv.FormatInt(old.Version, 10), &history); err != nil {
			return err
		}
		if !reflect.DeepEqual(history.Candidate, old) || history.Run.ID != first.Run.ID || !reflect.DeepEqual(history.Task, before) {
			t.Fatal("old blocked receipt not preserved", history)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	replay, err := RetryReviewInput(ctx, f.s, f.owner, "retry-input", before.ID, in)
	if err != nil || string(raw) != string(replay) {
		t.Fatal("retry replay consumed another allowance", err)
	}
	if _, err = RetryReviewInput(ctx, f.s, f.owner, "stale-retry", before.ID, in); err == nil {
		t.Fatal("stale retry accepted")
	}
	if _, err = f.s.TaskAction(ctx, f.owner, "resume-review", after.ID, "resume", domain.VersionRequest{ExpectedVersion: after.Version}); err != nil {
		t.Fatal(err)
	}
	next := f.claim(t, "review")
	if next.Run.Base != old.Commit || next.Run.CandidateID != old.ID || next.Run.Generation <= first.Run.Generation || next.Candidate.Review != nil {
		t.Fatal("retry did not create a new independent review of the same C", next)
	}
	_, err = f.s.Complete(ctx, f.node, "late-preflight-approval", first.Run.ID, domain.CompleteRequest{Generation: first.Run.Generation,
		Status: "succeeded", Quiescent: true, Review: &domain.Review{Candidate: old.Commit, Verdict: "approve", Summary: "late", Findings: []string{}}})
	if err != nil || f.candidate(t).Review != nil || f.snapshot(t).Tasks[0].CurrentRunID != next.Run.ID {
		t.Fatal("late old completion affected retry", err)
	}
	_, err = f.s.Complete(ctx, f.node, "fresh-review", next.Run.ID, domain.CompleteRequest{Generation: next.Run.Generation,
		Status: "succeeded", Quiescent: true, SessionID: "new-independent-model", ElapsedSeconds: 2,
		Review: &domain.Review{Candidate: old.Commit, Verdict: "approve", Summary: "New independent review", Findings: []string{}}})
	if err != nil || f.candidate(t).Review == nil || f.candidate(t).Review.RunID != next.Run.ID {
		t.Fatal("new independent review could not finish", err)
	}
}

func TestReviewInputRetryRejectsUnsafeStaleOrModelVerdicts(t *testing.T) {
	for _, scenario := range []string{
		"node", "chat", "task version", "candidate version", "commit", "review identity", "retry limit", "budget",
		"live process", "active task", "policy", "baseline", "goal", "native model", "missing preflight marker", "generation", "source not stopped",
	} {
		t.Run(scenario, func(t *testing.T) {
			f, task, c, assignment := blockedReviewInputFixture(t)
			in, actor := reviewInputRetryRequest(task, c), f.owner
			switch scenario {
			case "node":
				actor = f.node
			case "chat":
				actor = Actor{ID: "chat", Role: "chat"}
			case "task version":
				in.ExpectedVersion++
			case "candidate version":
				in.ExpectedCandidateVersion++
			case "commit":
				in.ExpectedCommit = c.Main
			case "review identity":
				in.ReviewRunID = c.SourceRunID
			}
			if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
				var run domain.Run
				if err := tx.Get("run", assignment.Run.ID, &run); err != nil {
					return err
				}
				switch scenario {
				case "retry limit":
					task.Retries = task.MaxRetries
				case "budget":
					task.UsedSeconds = task.BudgetSeconds
				case "live process":
					run.Quiescent = false
				case "active task":
					task.CurrentRunID = assignment.Run.ID
				case "policy":
					c.PolicyVersion++
				case "baseline":
					c.BaselineDigest = "other"
				case "goal":
					run.GoalAuthorizationVersion++
				case "native model":
					run.SessionID = "actual-review-model"
				case "missing preflight marker":
					run.Detail = "review model blocked"
				case "generation":
					run.Generation--
				case "source not stopped":
					var source domain.Run
					if err := tx.Get("run", c.SourceRunID, &source); err != nil {
						return err
					}
					source.Quiescent = false
					if err := tx.Put("run", source.ID, source); err != nil {
						return err
					}
				}
				if err := tx.Put("task", task.ID, task); err != nil {
					return err
				}
				if err := tx.Put("candidate", c.ID, c); err != nil {
					return err
				}
				return tx.Put("run", run.ID, run)
			}); err != nil {
				t.Fatal(err)
			}
			before := f.snapshot(t)
			if _, err := RetryReviewInput(context.Background(), f.s, actor, "unsafe-retry", task.ID, in); err == nil {
				t.Fatal("unsafe retry accepted")
			}
			after := f.snapshot(t)
			if !reflect.DeepEqual(before.Tasks, after.Tasks) || !reflect.DeepEqual(before.Candidates, after.Candidates) {
				t.Fatal("rejected retry changed task or code")
			}
		})
	}
}

func TestReviewInputRetryContentionConsumesOnlyOneAllowance(t *testing.T) {
	f, task, c, _ := blockedReviewInputFixture(t)
	in := reviewInputRetryRequest(task, c)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := RetryReviewInput(context.Background(), f.s, f.owner, "contending-"+strconv.Itoa(i), task.ID, in)
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	after := f.snapshot(t).Tasks[0]
	if success != 1 || after.Retries != task.Retries+1 || after.Version != task.Version+1 || after.Repairs != task.Repairs {
		t.Fatal("contending retry consumed multiple grants", success, task, after)
	}
}
