package app

import (
	"errors"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func (c ReviewIssues) record(tx *store.Tx, task domain.Task, run domain.Run, candidate domain.Candidate, review domain.Review) error {
	assigned, err := applicableReviewIssues(tx, task, run.PolicyVersion)
	if err != nil {
		return err
	}
	allowed := map[string]bool{}
	for _, issue := range assigned {
		allowed[issue.ID] = true
	}
	findings := review.Issues
	if run.ReviewContractVersion == 0 && review.Verdict != "approve" {
		for _, text := range review.Findings {
			title, _ := boundedFailureText(text, 512)
			evidence, _ := boundedFailureText(text, 2048)
			findings = append(findings, domain.ReviewFinding{Key: "legacy_" + Hash(text)[:24], Title: title, Evidence: evidence, Severity: "unknown",
				Trigger: "Legacy excerpt; consult the complete source Run review", Impact: "Requires structured triage"})
		}
	}
	for _, finding := range findings {
		id := "issue_" + Hash(task.ID + "/internal/" + finding.Key)[:32]
		var issue domain.ReviewIssue
		err := tx.Get("review_issue", id, &issue)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if errors.Is(err, domain.ErrNotFound) {
			issue = domain.ReviewIssue{ID: id, TaskID: task.ID, Source: "internal", FirstCandidate: candidate.Commit, CreatedAt: c.Service.Now()}
		}
		issue.Finding, issue.State, issue.LastCandidate, issue.SourceRunID = finding, "open", candidate.Commit, run.ID
		issue.RulesDigest = run.ReviewRulesDigest
		issue.ResolutionCommit, issue.ResolutionReason, issue.ResolvedBy = "", "", ""
		if err := c.save(tx, &issue, Actor{ID: "center"}, "review.issue_observed"); err != nil {
			return err
		}
	}
	for _, resolution := range review.Resolutions {
		var issue domain.ReviewIssue
		if err := tx.Get("review_issue", resolution.IssueID, &issue); err != nil {
			return err
		}
		if issue.TaskID != task.ID || issue.Source != "internal" || !allowed[issue.ID] {
			return domain.Fail("scope", "review cannot resolve another task or an external provider's issue")
		}
		if err := version(issue.Version, resolution.ExpectedVersion); err != nil {
			return err
		}
		for _, finding := range findings {
			if finding.Key == issue.Finding.Key {
				return domain.Fail("invalid_result", "same issue cannot be observed and resolved in one review")
			}
		}
		issue.State, issue.ResolutionCommit, issue.ResolutionReason = resolution.Disposition, candidate.Commit, resolution.Reason
		issue.LastCandidate, issue.ResolvedBy = candidate.Commit, run.ID
		issue.SourceRunID = run.ID
		issue.RulesDigest = run.ReviewRulesDigest
		if err := c.save(tx, &issue, Actor{ID: "center"}, "review.issue_resolved"); err != nil {
			return err
		}
	}
	if run.ReviewContractVersion == domain.ReviewContractVersion && review.Verdict == "approve" {
		return reviewLedgerReady(tx, task.ID, candidate.Commit)
	}
	return nil
}

func reviewLedgerReady(tx *store.Tx, taskID, commit string) error {
	var task domain.Task
	if err := tx.Get("task", taskID, &task); err != nil {
		return err
	}
	project, err := getProject(tx)
	if err != nil {
		return err
	}
	issues, err := applicableReviewIssues(tx, task, project.PolicyVersion)
	if err != nil {
		return err
	}
	for _, issue := range issues {
		if domain.ReviewIssueBlocks(issue, commit) {
			return domain.Fail("review_unresolved", "review has unresolved or unverified findings on the fixed candidate")
		}
	}
	return nil
}
