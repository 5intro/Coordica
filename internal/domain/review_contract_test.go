package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReviewContractKeepsEmptyArraysAndRejectsAmbiguousDispositions(t *testing.T) {
	v := Review{Candidate: strings.Repeat("a", 40), Verdict: "approve", Summary: "0 findings", Findings: []string{}, RulesDigest: "fixed", Issues: []ReviewFinding{}, Resolutions: []ReviewResolution{}}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var got Review
	if err = json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if err = ValidateStructuredReview(got, "fixed"); err != nil {
		t.Fatal("empty arrays lost over HTTP", err, string(b))
	}
	legacy, _ := json.Marshal(Review{Candidate: v.Candidate, Findings: []string{}})
	if strings.Contains(string(legacy), "issues") || strings.Contains(string(legacy), "rules_digest") {
		t.Fatal("old contract gained unknown fields")
	}
	for _, mode := range []string{"digest", "missing issues", "risk claim", "missing evidence", "duplicate resolution", "approval with findings", "changes without evidence"} {
		bad := v
		switch mode {
		case "digest":
			bad.RulesDigest = "wrong"
		case "missing issues":
			bad.Issues = nil
		case "risk claim":
			bad.Resolutions = []ReviewResolution{{IssueID: "x", ExpectedVersion: 1, Disposition: "accepted_risk", Reason: "model accepted"}}
		case "missing evidence":
			bad.Verdict = "changes_requested"
			bad.Issues = []ReviewFinding{{Key: "k", Severity: "P1", Title: "maybe"}}
		case "duplicate resolution":
			bad.Resolutions = []ReviewResolution{{IssueID: "x", ExpectedVersion: 1, Disposition: "fixed", Reason: "reproduced"}, {IssueID: "x", ExpectedVersion: 1, Disposition: "fixed", Reason: "again"}}
		case "approval with findings":
			bad.Issues = []ReviewFinding{{Key: "k", Severity: "P1", Title: "defect", Trigger: "retry", Evidence: "trace", Impact: "duplicate payment"}}
		case "changes without evidence":
			bad.Verdict = "changes_requested"
		}
		if ValidateStructuredReview(bad, "fixed") == nil {
			t.Fatal("invalid review accepted", mode)
		}
	}
}

func TestReviewRulesValidatePathsAndPreserveWindowsText(t *testing.T) {
	for _, mode := range []string{"valid CRLF", "invalid base", "unsafe path", "unsorted changes", "duplicate changes"} {
		r := ReviewRules{Version: 1, Base: strings.Repeat("a", 40), Files: []ReviewRuleFile{{Path: "REVIEW.md", Text: "Verify retries\r\n"}}, Changed: []string{"REVIEW.md"}}
		switch mode {
		case "invalid base":
			r.Base = strings.Repeat("z", 40)
		case "unsafe path":
			r.Changed = []string{"../REVIEW.md"}
		case "unsorted changes":
			r.Changed = []string{"SECURITY.md", "REVIEW.md"}
		case "duplicate changes":
			r.Changed = []string{"REVIEW.md", "REVIEW.md"}
		}
		r.Digest = ReviewRulesDigest(r)
		err := ValidateReviewRules(r, r.Base)
		if (err == nil) != (mode == "valid CRLF") {
			t.Fatal(mode, err)
		}
	}
}

func TestReviewRulesPinContentAndBaseAndNeverInheritOldRiskApproval(t *testing.T) {
	r := ReviewRules{Version: ReviewContractVersion, Base: strings.Repeat("a", 40), Files: []ReviewRuleFile{{Path: "REVIEW.md", Text: "Verify retries\n"}}, Changed: []string{}}
	r.Digest = ReviewRulesDigest(r)
	if err := ValidateReviewRules(r, r.Base); err != nil {
		t.Fatal(err)
	}
	r.Files[0].Text = "Skip retries"
	if ValidateReviewRules(r, r.Base) == nil {
		t.Fatal("rule content changed without digest change")
	}
	issue := ReviewIssue{State: "accepted_risk", ResolutionCommit: "old"}
	if !ReviewIssueBlocks(issue, "new") || ReviewIssueBlocks(issue, "old") {
		t.Fatal("risk disposition crossed its exact commit")
	}
}
