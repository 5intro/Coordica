package domain

import (
	"testing"
	"time"
)

func TestExternalReviewVerdictRequiresBothAuthoritativeSources(t *testing.T) {
	policy := ExternalReviewPolicy{Provider: "codex", ActorID: 42, AppID: 99, Kinds: []string{"security_review"},
		CheckNames: map[string]string{"security_review": "Codex security"}}
	valid := ExternalReview{Provider: "codex", Kind: "security_review", ActorID: 42, AppID: 99, Commit: "fixed",
		CheckID: 20, CheckName: "Codex security", CheckState: "success", ReportID: 11,
		ReportUpdatedAt: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC), ReportState: "success", State: "success"}
	if !ExternalReviewsReady([]ExternalReviewPolicy{policy}, []ExternalReview{valid}, "fixed") {
		t.Fatal("complete matching verdict rejected")
	}
	for _, field := range []string{"check id", "check name", "check state", "report state", "report version", "report id", "actor", "app", "commit", "kind"} {
		t.Run(field, func(t *testing.T) {
			r := valid
			switch field {
			case "check id":
				r.CheckID = 0
			case "check name":
				r.CheckName = "Other check"
			case "check state":
				r.CheckState = "pending"
			case "report state":
				r.ReportState = "failure"
			case "report version":
				r.ReportUpdatedAt = time.Time{}
			case "report id":
				r.ReportID = 0
			case "actor":
				r.ActorID++
			case "app":
				r.AppID++
			case "commit":
				r.Commit = "old"
			case "kind":
				r.Kind = "code_review"
			}
			if ExternalReviewsReady([]ExternalReviewPolicy{policy}, []ExternalReview{r}, "fixed") {
				t.Fatal("unsubstantiated combined success granted approval", field)
			}
		})
	}
	valid.State, valid.CheckState, valid.ReportState = "failure", "failure", "failure"
	if !ExternalReviewVerdict(policy, valid, "fixed", "failure") || ExternalReviewsReady([]ExternalReviewPolicy{policy}, []ExternalReview{valid}, "fixed") {
		t.Fatal("coherent finding verdict lost or granted approval")
	}
}
