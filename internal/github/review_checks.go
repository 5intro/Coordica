package github

import "coordica/internal/domain"

// Comments are deletable by repository writers. They can supply finding
// context, but a successful comment alone never grants publication authority.
func bindReviewCheck(policy domain.ExternalReviewPolicy, kind string, report domain.ExternalReview, checks []domain.RemoteCheck) domain.ExternalReview {
	report.CheckName = policy.CheckNames[kind]
	report.ReportState = report.State
	check := reviewCheckSource(policy, kind, checks)
	if check.ID > 0 {
		report.CheckID = check.ID
		report.CheckState = check.State
		if check.State == "success" && report.State == "success" || check.State == "failure" && report.State == "failure" {
			return report
		}
		if report.State != "pending" || report.Summary == "" {
			report.Summary = "Trusted App Check Run and structured review evidence have not established the same current verdict"
		}
		report.State, report.Evidence = "pending", ""
		return report
	}
	report.State, report.Evidence = "pending", ""
	report.Summary = "Trusted App-owned review Check Run is missing; comments cannot approve integration"
	return report
}

func reviewCheckSource(policy domain.ExternalReviewPolicy, kind string, checks []domain.RemoteCheck) domain.RemoteCheck {
	for _, check := range checks {
		if check.Name == policy.CheckNames[kind] && check.AppID == policy.AppID && check.ID > 0 {
			return check
		}
	}
	return domain.RemoteCheck{}
}
