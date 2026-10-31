package github

import "coordica/internal/domain"

// Comments are deletable by repository writers. They can supply finding
// context, but a successful comment alone never grants publication authority.
func bindReviewCheck(policy domain.ExternalReviewPolicy, kind string, report domain.ExternalReview, checks []domain.RemoteCheck) domain.ExternalReview {
	report.CheckName = policy.CheckNames[kind]
	report.ReportState = report.State
	for _, check := range checks {
		if check.Name != report.CheckName || check.AppID != policy.AppID || check.ID <= 0 {
			continue
		}
		report.CheckID = check.ID
		report.CheckState = check.State
		if check.State == "success" && report.State == "success" || check.State == "failure" && report.State == "failure" {
			return report
		}
		report.State, report.Evidence = "pending", ""
		report.Summary = "Trusted App Check Run and structured review evidence have not established the same current verdict"
		return report
	}
	report.State, report.Evidence = "pending", ""
	report.Summary = "Trusted App-owned review Check Run is missing; comments cannot approve integration"
	return report
}
