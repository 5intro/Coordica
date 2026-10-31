package app

import "coordica/internal/domain"

// Deterministic reviewer fixture uses the negotiated wire contract, including
// explicit per-issue dispositions. Production never synthesizes these proofs.
func testReview(a domain.Assignment, v domain.Review) *domain.Review {
	if a.Run.ReviewContractVersion != domain.ReviewContractVersion {
		return &v
	}
	v.RulesDigest = a.Run.ReviewRulesDigest
	v.Issues = []domain.ReviewFinding{}
	v.Resolutions = []domain.ReviewResolution{}
	if v.Summary != "Complete fixed review input is unavailable" {
		for _, finding := range v.Findings {
			v.Issues = append(v.Issues, domain.ReviewFinding{Key: "fixture_" + Hash(finding)[:16], Severity: "P2", Title: finding, Trigger: "Fixture scenario", Evidence: finding, Impact: "Contract not satisfied"})
		}
	}
	v.Findings = []string{}
	if v.Verdict == "approve" && a.ReviewContext != nil {
		for _, issue := range a.ReviewContext.Issues {
			if issue.Source == "internal" && domain.ReviewIssueBlocks(issue, v.Candidate) {
				v.Resolutions = append(v.Resolutions, domain.ReviewResolution{IssueID: issue.ID, ExpectedVersion: issue.Version, Disposition: "fixed", Reason: "Fixture inspected the fixed candidate and validated the regression"})
			}
		}
	}
	return &v
}
