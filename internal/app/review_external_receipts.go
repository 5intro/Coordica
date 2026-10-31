package app

import (
	"errors"
	"fmt"
	"time"

	"coordica/internal/domain"
	"coordica/internal/store"
)

// One monotonic receipt per task, fixed head and enrolled App/check kind.
// A vanished or regressed terminal result cannot resurrect its former green.
type externalReviewReceipt struct {
	CheckID           int64     `json:"check_id"`
	State             string    `json:"state"`
	Invalidated       bool      `json:"invalidated"`
	ReportID          int64     `json:"report_id"`
	ReportUpdatedAt   time.Time `json:"report_updated_at"`
	ReportState       string    `json:"report_state"`
	ReportInvalidated bool      `json:"report_invalidated"`
}

func (c ReviewIssues) fenceExternal(tx *store.Tx, task domain.Task, candidate domain.Candidate, facts *domain.PullRequestStatus) error {
	project, err := getProject(tx)
	if err != nil || !domain.GitHubPublication(project) {
		return err
	}
	policies := project.Publication.ReviewProviders
	normalized := []domain.ExternalReview{}
	for _, policy := range policies {
		for _, kind := range policy.Kinds {
			report := domain.ExternalReview{Provider: policy.Provider, Kind: kind, ActorID: policy.ActorID, AppID: policy.AppID,
				Commit: candidate.Commit, CheckName: policy.CheckNames[kind], State: "pending", Summary: "Current trusted App review evidence missing or invalid"}
			matches := 0
			for _, incoming := range facts.ExternalReviews {
				if incoming.Provider == policy.Provider && incoming.Kind == kind && incoming.ActorID == policy.ActorID &&
					incoming.AppID == policy.AppID && incoming.Commit == candidate.Commit && incoming.CheckName == report.CheckName {
					report = incoming
					matches++
				}
			}
			if matches != 1 || domain.ValidateExternalReviewPolicy([]domain.ExternalReviewPolicy{policy}) != nil {
				report.CheckID, report.ReportID, report.ReportUpdatedAt = 0, 0, time.Time{}
			}
			if !domain.ExternalReviewVerdict(policy, report, candidate.Commit, "success") &&
				!domain.ExternalReviewVerdict(policy, report, candidate.Commit, "failure") {
				report.State, report.Evidence = "pending", ""
			}
			id := Hash(fmt.Sprintf("%s/%s/%s/%s/%d/%d/%s", task.ID, candidate.Commit, policy.Provider, kind, policy.ActorID, policy.AppID, policy.CheckNames[kind]))
			if err := c.fenceExternalReceipt(tx, id, task.ID, &report); err != nil {
				return err
			}
			normalized = append(normalized, report)
		}
	}
	if len(facts.ExternalReviews) > len(normalized) {
		return domain.Fail("invalid_result", "unexpected external review source")
	}
	facts.ExternalReviews = normalized
	if !domain.ExternalReviewsReady(policies, normalized, candidate.Commit) {
		facts.MergeReady = false
		return queueTaskPullReviewRevocations(tx, task.ID)
	}
	return nil
}

func (c ReviewIssues) fenceExternalReceipt(tx *store.Tx, id, task string, report *domain.ExternalReview) error {
	var receipt externalReviewReceipt
	err := tx.Get("external_review_receipt", id, &receipt)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	previous := receipt
	checkOK := receipt.acceptCheck(*report)
	reportOK := receipt.acceptReport(*report)
	if !checkOK || !reportOK {
		report.State, report.Evidence = "pending", ""
		report.Summary = "External review evidence regressed or disappeared; fresh trusted source evidence is required"
	}
	if receipt.CheckID == 0 && receipt.ReportID == 0 || receipt == previous {
		return nil
	}
	if err := tx.Put("external_review_receipt", id, receipt); err != nil {
		return err
	}
	return c.Service.audit(tx, Actor{ID: "center"}, "review.external_receipt", task, fmt.Sprintf("%s/%d/%s", report.Kind, receipt.CheckID, receipt.State))
}

func (r *externalReviewReceipt) acceptCheck(report domain.ExternalReview) bool {
	state := report.CheckState
	if state != "success" && state != "failure" {
		state = "pending"
	}
	if report.CheckID > r.CheckID {
		r.CheckID, r.State, r.Invalidated = report.CheckID, state, false
	} else if r.CheckID > 0 {
		terminal := r.State == "success" || r.State == "failure"
		if report.CheckID < r.CheckID || r.Invalidated || terminal && state != r.State {
			r.Invalidated = r.Invalidated || terminal
			return false
		}
		r.State = state
	}
	return report.CheckID > 0 && !r.Invalidated
}

func (r *externalReviewReceipt) acceptReport(report domain.ExternalReview) bool {
	if report.ReportID <= 0 || report.ReportUpdatedAt.IsZero() {
		r.ReportInvalidated = r.ReportInvalidated || r.ReportState == "success" || r.ReportState == "failure"
		return false
	}
	stamp := report.ReportUpdatedAt.UTC()
	newer := stamp.After(r.ReportUpdatedAt) || stamp.Equal(r.ReportUpdatedAt) && report.ReportID > r.ReportID
	if newer {
		r.ReportID, r.ReportUpdatedAt, r.ReportState, r.ReportInvalidated = report.ReportID, stamp, report.ReportState, false
		return true
	}
	if report.ReportID != r.ReportID || !stamp.Equal(r.ReportUpdatedAt) || report.ReportState != r.ReportState || r.ReportInvalidated {
		r.ReportInvalidated = r.ReportInvalidated || r.ReportState == "success" || r.ReportState == "failure" || stamp.Equal(r.ReportUpdatedAt)
		return false
	}
	return true
}
