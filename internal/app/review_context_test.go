package app

import (
	"context"
	"strings"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func nextReview(t *testing.T, change func(*domain.Task, *domain.Candidate, *domain.Run)) (*serviceFixture, domain.Task) {
	t.Helper()
	f, old, _ := failureReviewFixture(t, nil, false)
	var task domain.Task
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		if err := tx.Get("task", old.TaskID, &task); err != nil {
			return err
		}
		// The fixture's returned candidate predates the completed review.
		if err := tx.Get("candidate", old.ID, &old); err != nil {
			return err
		}
		var previous domain.Run
		if err := tx.Get("run", old.Review.RunID, &previous); err != nil {
			return err
		}
		if change != nil {
			change(&task, &old, &previous)
		}
		if err := tx.Put("candidate", old.ID, old); err != nil {
			return err
		}
		if err := tx.Put("run", previous.ID, previous); err != nil {
			return err
		}
		task.HoldReason, task.CurrentRunID, task.CurrentCandidateID = "", "", "new-candidate"
		task.State = "awaiting_integration"
		current := old
		current.ID, current.Commit, current.State, current.Review = task.CurrentCandidateID, strings.Repeat("e", 40), "reviewing", nil
		current.AuthorizationVersion = task.AuthorizationVersion
		if err := tx.Put("candidate", current.ID, current); err != nil {
			return err
		}
		return tx.Put("task", task.ID, task)
	}); err != nil {
		t.Fatal(err)
	}
	return f, task
}

func TestReviewClaimCarriesFixedPreviousFindings(t *testing.T) {
	f, task := nextReview(t, nil)
	a := f.claim(t, "second-independent-review")
	if a == nil || a.Run.Kind != "review" || a.Task.ID != task.ID || a.ReviewContext == nil ||
		a.ReviewContext.Round != 2 || a.ReviewContext.PreviousCandidate != strings.Repeat("c", 40) ||
		len(a.ReviewContext.Findings) != 1 || a.ReviewContext.Findings[0] != "missing input" || a.Candidate.Review != nil {
		t.Fatal("review inherited approval or lost historical findings", a)
	}
}

func TestReviewHistoryDoesNotCrossAuthorityOrUseUnstoppedEvidence(t *testing.T) {
	for _, mode := range []string{"authority", "policy", "goal", "requirements", "unstopped", "wrong candidate"} {
		t.Run(mode, func(t *testing.T) {
			f, task := nextReview(t, func(task *domain.Task, c *domain.Candidate, r *domain.Run) {
				switch mode {
				case "authority":
					task.AuthorizationVersion++
				case "policy":
					c.PolicyVersion++
				case "goal":
					r.GoalAuthorizationVersion++
				case "requirements":
					r.RequirementRevisions = map[string]int64{"other": 1}
				case "unstopped":
					r.Quiescent = false
				case "wrong candidate":
					r.CandidateID = "unrelated"
				}
			})
			if err := f.s.DB.View(context.Background(), func(tx *store.Tx) error {
				r := domain.Run{Kind: "review", CandidateID: task.CurrentCandidateID, PolicyVersion: 1,
					BaselineVersion: task.BaselineVersion, BaselineDigest: task.BaselineDigest,
					GoalVersion: task.GoalVersion, GoalAuthorizationVersion: task.GoalAuthorizationVersion,
					RequirementRevisions: task.RequirementRevisions}
				got, err := assignedReviewContext(tx, task, r)
				if err != nil {
					return err
				}
				if got != nil {
					t.Fatal("historical findings crossed their source boundary", got)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOversizedReviewHistoryBlocksBeforeMintingRun(t *testing.T) {
	f, task := nextReview(t, func(_ *domain.Task, c *domain.Candidate, _ *domain.Run) {
		c.Review.Findings = []string{strings.Repeat("evidence", domain.MaxAssignmentContextBytes)}
	})
	count := len(f.snapshot(t).Runs)
	if a := f.claim(t, "oversized-history"); a != nil {
		t.Fatal("large feedback silently truncated", a)
	}
	if state := requirementTask(t, f, task.ID); state.HoldReason != "human_decision" || state.CurrentRunID != "" || len(f.snapshot(t).Runs) != count {
		t.Fatal("oversized history partially claimed resources", state)
	}
}
