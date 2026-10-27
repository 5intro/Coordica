package app

import (
	"context"
	"encoding/json"
	"maps"
	"strconv"
	"strings"
	"time"

	"coordica/internal/domain"
	"coordica/internal/store"
)

type reviewInputRetryReceipt struct {
	Task      domain.Task      `json:"task"`
	Candidate domain.Candidate `json:"candidate"`
	Run       domain.Run       `json:"run"`
	ActorID   string           `json:"actor_id"`
	Reason    string           `json:"reason"`
	CreatedAt time.Time        `json:"created_at"`
}

// The former blocked result is kept as history. Reopening the same C grants
// only one bounded new review attempt and leaves execution explicitly paused.
func RetryReviewInput(ctx context.Context, s *Service, a Actor, key, id string, in domain.ReviewInputRetryRequest) (json.RawMessage, error) {
	if err := owner(a); err != nil {
		return nil, err
	}
	if err := domain.ValidateReviewInputRetry(in); err != nil {
		return nil, err
	}
	return s.mutate(ctx, a, key, "task.review-input-retry."+id, in, func(tx *store.Tx) (any, error) {
		return retryReviewInput(tx, s, a, id, in, "paused", "Owner requested bounded material retry")
	})
}

// Both manual and automatic retries use this exact fixed-source transition.
// The caller owns authorization; this helper grants no new scope or budget.
func retryReviewInput(tx *store.Tx, s *Service, actor Actor, id string, in domain.ReviewInputRetryRequest, hold, detail string) (any, error) {
	var task domain.Task
	if err := tx.Get("task", id, &task); err != nil {
		return nil, err
	}
	if err := version(task.Version, in.ExpectedVersion); err != nil {
		return nil, err
	}
	if task.State != "awaiting_integration" || task.CurrentRunID != "" || task.ReservedSeconds != 0 || task.ApprovedVersion == 0 ||
		task.CurrentCandidateID != in.ExpectedCandidateID || (task.HoldReason != "human_decision" && task.HoldReason != "paused" && task.HoldReason != "failure_assessment") {
		return nil, domain.Fail("conflict", "a stopped approved task with a blocked current review is required")
	}
	if err := requireQuiescence(tx, id); err != nil {
		return nil, err
	}
	if err := baselineGate(tx, nil, false); err != nil {
		return nil, err
	}
	if err := goalGate(tx, &task, false); err != nil {
		return nil, err
	}
	if task.Retries >= task.MaxRetries || task.UsedSeconds >= task.BudgetSeconds {
		return nil, domain.Fail("budget", "finite review retry allowance exhausted")
	}
	project, err := getProject(tx)
	if err != nil {
		return nil, err
	}
	var c domain.Candidate
	if err = tx.Get("candidate", in.ExpectedCandidateID, &c); err != nil {
		return nil, err
	}
	if err = version(c.Version, in.ExpectedCandidateVersion); err != nil {
		return nil, err
	}
	if c.Commit != in.ExpectedCommit || c.Review == nil || c.Review.RunID != in.ReviewRunID || c.Review.Verdict != "blocked" {
		return nil, domain.Fail("conflict", "exact fixed blocked review required")
	}
	if err = rejectedRepairSource(tx, task, project, c); err != nil {
		return nil, err
	}
	var run domain.Run
	if err = tx.Get("run", in.ReviewRunID, &run); err != nil {
		return nil, err
	}
	if !reviewInputPreflightStopped(task, c, run) {
		return nil, domain.Fail("conflict", "only a stopped pre-Agent material failure can use this retry")
	}
	receipt := reviewInputRetryReceipt{task, c, run, actor.ID, in.Reason, s.Now()}
	if err = tx.Put("review_input_retry_history", c.ID+"_"+strconv.FormatInt(c.Version, 10), receipt); err != nil {
		return nil, err
	}
	c.State, c.Detail, c.Review = "reviewing", detail, nil
	c.Version++
	task.Retries++
	task.Version++
	task.HoldReason = hold
	if err = tx.Put("candidate", c.ID, c); err != nil {
		return nil, err
	}
	if err = tx.Put("task", id, task); err != nil {
		return nil, err
	}
	return task, s.audit(tx, actor, "review.input_retry", c.ID, run.ID+": "+in.Reason)
}

// Legacy receipts have no typed preflight field. Require all recorded facts,
// including the Runner's pre-execution marker, rather than absence of a session
// alone. These are trusted-host process receipts, not OS isolation attestations.
func reviewInputPreflightStopped(t domain.Task, c domain.Candidate, r domain.Run) bool {
	return r.TaskID == t.ID && r.Kind == "review" && r.CandidateID == c.ID && r.Base == c.Commit &&
		r.State == "succeeded" && r.Quiescent && r.FinishedAt != nil && r.SessionID == "" && r.Generation == t.Generation &&
		r.AuthorizationVersion == t.AuthorizationVersion && r.PolicyVersion == c.PolicyVersion &&
		r.GoalVersion == t.GoalVersion && r.GoalAuthorizationVersion == t.GoalAuthorizationVersion &&
		r.BaselineVersion == t.BaselineVersion && r.BaselineDigest == t.BaselineDigest && maps.Equal(r.RequirementRevisions, t.RequirementRevisions) &&
		strings.HasPrefix(r.Detail, "review blocked before agent execution: ") && c.Review.Summary == "Complete fixed review input is unavailable"
}
