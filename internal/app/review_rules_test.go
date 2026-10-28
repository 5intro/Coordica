package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/gitops"
)

func TestReviewRulesUseMainAndRequireApprovalForCandidateRuleChanges(t *testing.T) {
	f := newITFixture(t)
	seed := filepath.Join(filepath.Dir(f.s.Dir), "seed")
	if err := os.WriteFile(filepath.Join(seed, "src", "REVIEW.md"), []byte("Verify every retry against idempotency.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	main, err := f.g.CommitAll(context.Background(), seed, "Approved rules", gitops.Identity{})
	if err != nil {
		t.Fatal(err)
	}
	itGit(t, "git", seed, "push", "--", f.remote, string(main)+":refs/heads/main")
	f.implement(t, "src/REVIEW.md")
	c := f.candidate(t)
	if c.ReviewRules == nil || c.ReviewRules.Base != string(main) || !c.Protected || len(c.ReviewRules.Changed) != 1 ||
		len(c.ReviewRules.Files) != 1 || !strings.Contains(c.ReviewRules.Files[0].Text, "idempotency") {
		t.Fatal("candidate weakened its own rules", c)
	}
	a := f.claim(t, "review")
	v := testReview(a, domain.Review{Candidate: c.Commit, Verdict: "approve", Summary: "0 actionable findings", Findings: []string{}})
	_, err = f.s.Complete(context.Background(), f.node, "rule-review", a.Run.ID, domain.CompleteRequest{Generation: a.Run.Generation, Status: "succeeded", Quiescent: true, Review: v})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	c = f.candidate(t)
	if c.State == "landed" {
		t.Fatal("rule change bypassed Owner approval")
	}
	if _, err := f.s.ApproveCandidate(context.Background(), f.owner, "approve-rules", c.ID, domain.VersionRequest{ExpectedVersion: c.Version}); err != nil {
		t.Fatal(err)
	}
	if err := f.w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	landed := f.candidate(t)
	if landed.State != "landed" {
		t.Fatal(landed)
	}
	rules, err := pinnedReviewRules(context.Background(), f.g, f.w.repo(), domain.Candidate{Main: landed.Commit}, []gitops.Change{{Status: "M", NewPath: "src/next.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rules.Files) != 1 || rules.Files[0].Text != "implemented\n" {
		t.Fatal("approved new rules did not become effective", rules)
	}
}
