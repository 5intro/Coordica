package app

import (
	"errors"
	"fmt"

	"coordica/internal/domain"
	"coordica/internal/store"
)

// An already merged PR is an observed write, not permission to acknowledge
// integration. Keep its raw evidence while the configured provider gate waits.
func (c ReviewIssues) mergedEvidenceReady(tx *store.Tx, task domain.Task, candidate *domain.Candidate, facts *domain.PullRequestStatus, commit string) (bool, error) {
	if facts == nil {
		return false, nil
	}
	project, err := getProject(tx)
	if err != nil {
		return false, err
	}
	candidate.PullRequest = facts
	if err := c.observeExternal(tx, task, *candidate, *facts); err != nil {
		return false, err
	}
	return domain.GitHubPublication(project) && domain.ExternalReviewsReady(project.Publication.ReviewProviders, facts.ExternalReviews, commit), nil
}

// External summaries are gate-level evidence, not invented per-line findings.
// Only the configured provider can clear them; an internal Agent cannot waive
// an external finding by changing its local issue disposition.
func (c ReviewIssues) observeExternal(tx *store.Tx, task domain.Task, candidate domain.Candidate, facts domain.PullRequestStatus) error {
	project, err := getProject(tx)
	if err != nil {
		return err
	}
	for _, report := range facts.ExternalReviews {
		trusted := false
		if project.Publication != nil {
			for _, p := range project.Publication.ReviewProviders {
				if p.Provider == report.Provider && p.ActorID == report.ActorID && p.AppID == report.AppID {
					for _, kind := range p.Kinds {
						trusted = trusted || kind == report.Kind
					}
				}
			}
		}
		if !trusted {
			return domain.Fail("invalid_result", "external review source was not configured by Owner")
		}
		if report.Commit != candidate.Commit {
			return domain.Fail("stale_run", "external review describes a different candidate")
		}
		source := fmt.Sprintf("external/%s/%s/%d/%d", report.Provider, report.Kind, report.ActorID, report.AppID)
		evidence := report.Summary + "\n" + report.URL + "\n" + report.Evidence
		id := "issue_" + Hash(task.ID + "/" + source)[:32]
		var issue domain.ReviewIssue
		err := tx.Get("review_issue", id, &issue)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if report.State == "failure" {
			if err != nil {
				issue = domain.ReviewIssue{ID: id, TaskID: task.ID, Source: source, FirstCandidate: candidate.Commit, CreatedAt: c.Service.Now()}
			}
			if issue.State == "open" && issue.LastCandidate == candidate.Commit && issue.Finding.Evidence == evidence {
				continue
			}
			issue.Finding = domain.ReviewFinding{Key: report.Kind, Severity: "unknown", Title: report.Kind + " · external findings",
				Trigger: "External review gate on fixed commit", Evidence: evidence, Impact: "Blocks integration until this trusted provider clears its findings"}
			issue.State, issue.LastCandidate = "open", candidate.Commit
			issue.ResolutionCommit, issue.ResolutionReason, issue.ResolvedBy = "", "", ""
		} else if report.State == "success" && err == nil && (issue.State == "open" || issue.ResolutionCommit != candidate.Commit) {
			issue.State, issue.LastCandidate, issue.ResolutionCommit = "resolved_external", candidate.Commit, candidate.Commit
			issue.ResolutionReason, issue.ResolvedBy = report.Summary+"\n"+report.URL, source
		} else {
			continue
		}
		if err := c.save(tx, &issue, Actor{ID: "center"}, "review.external_observed"); err != nil {
			return err
		}
	}
	return nil
}
