package app

import (
	"context"
	"encoding/json"
	"fmt"

	"coordica/internal/domain"
	"coordica/internal/store"
)

type ReviewIssues struct{ Service *Service }

func (c ReviewIssues) List(ctx context.Context, a Actor, q domain.ReviewIssueQuery) (domain.ReviewIssuePage, error) {
	var page domain.ReviewIssuePage
	if err := owner(a); err != nil {
		return page, err
	}
	if !domain.ChatIdentifier.MatchString(q.TaskID) || !domain.ChatText(q.After, 128, false) || q.Limit < 1 || q.Limit > 64 {
		return page, domain.Fail("invalid_input", "task and a 1..64 issue page required")
	}
	err := c.Service.DB.View(ctx, func(tx *store.Tx) (err error) { page, err = store.ReviewIssues(tx, q); return })
	return page, err
}

func (c ReviewIssues) Decide(ctx context.Context, a Actor, key, id string, in domain.ReviewIssueDecision) (json.RawMessage, error) {
	if err := owner(a); err != nil {
		return nil, err
	}
	if !domain.ChatText(in.Reason, 2048, true) || (in.Disposition != "accepted_risk" && in.Disposition != "false_positive" && in.Disposition != "superseded") {
		return nil, domain.Fail("invalid_input", "explicit disposition and reason required")
	}
	return c.Service.mutate(ctx, a, key, "review.issue.decide."+id, in, func(tx *store.Tx) (any, error) {
		var issue domain.ReviewIssue
		if err := tx.Get("review_issue", id, &issue); err != nil {
			return nil, err
		}
		if err := version(issue.Version, in.ExpectedVersion); err != nil {
			return nil, err
		}
		if issue.Source != "internal" {
			return nil, domain.Fail("scope", "external issues must be cleared by their configured provider")
		}
		var candidate domain.Candidate
		var task domain.Task
		if err := tx.Get("candidate", in.CandidateID, &candidate); err != nil {
			return nil, err
		}
		if err := tx.Get("task", issue.TaskID, &task); err != nil {
			return nil, err
		}
		if candidate.TaskID != issue.TaskID || candidate.Commit != in.Commit || in.Commit == "" ||
			candidate.State == "landing" || candidate.State == "landed" || domain.TaskFinished(task) {
			return nil, domain.Fail("candidate_changed", "issue decision must name its current fixed candidate")
		}
		if err := issueCandidateCurrent(tx, task, candidate); err != nil {
			return nil, err
		}
		issue.State, issue.ResolutionCommit, issue.ResolutionReason, issue.ResolvedBy = in.Disposition, in.Commit, in.Reason, a.ID
		if err := c.save(tx, &issue, a, "review.issue_decided"); err != nil {
			return nil, err
		}
		if in.RequestRecheck {
			if err := c.recheck(tx, a, task, candidate, in); err != nil {
				return nil, err
			}
		}
		return issue, nil
	})
}

func (c ReviewIssues) save(tx *store.Tx, v *domain.ReviewIssue, a Actor, event string) error {
	v.Version++
	v.UpdatedAt = c.Service.Now()
	if err := tx.Put("review_issue", v.ID, *v); err != nil {
		return err
	}
	if err := tx.Put("review_issue_history", fmt.Sprintf("%s_v%d", v.ID, v.Version), *v); err != nil {
		return err
	}
	return c.Service.audit(tx, a, event, v.ID, v.State+": "+v.LastCandidate)
}

func taskReviewIssues(tx *store.Tx, taskID string) ([]domain.ReviewIssue, error) {
	page, err := store.ReviewIssues(tx, domain.ReviewIssueQuery{TaskID: taskID, Limit: 64})
	if err == nil && page.Next != "" {
		err = domain.Fail("context_limit", "complete issue context exceeds 64 records; explicit task review required")
	}
	return page.Items, err
}
