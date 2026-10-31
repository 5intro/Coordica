package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"coordica/internal/codequality"
	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/store"
)

func qualityCandidate(t *testing.T, f *itFixture) domain.Candidate {
	t.Helper()
	text := "export const value = '" + strings.Repeat("x", codequality.MaxLine+1) + "';\n"
	return qualityCandidateSource(t, f, "src/feature.js", []byte(text))
}

func qualityCandidateSource(t *testing.T, f *itFixture, path string, text []byte) domain.Candidate {
	t.Helper()
	ctx := context.Background()
	a := f.claim(t, "implement")
	bundle, err := f.w.InputBundle(ctx, a.Run)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "work")
	if err = f.g.CloneSnapshot(ctx, bundle, gitops.OID(a.Run.Base), dir); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, filepath.FromSlash(path)), text, 0600); err != nil {
		t.Fatal(err)
	}
	head, err := f.g.CommitAll(ctx, dir, "oversized source", gitops.Identity{})
	if err != nil {
		t.Fatal(err)
	}
	material := filepath.Join(t.TempDir(), "head.bundle")
	if _, err = f.g.CreateBundle(ctx, dir, gitops.OID(a.Run.Base), head, material); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(material)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	artifact, err := f.s.UploadArtifact(ctx, f.node, a.Run.ID, file)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.s.Complete(ctx, f.node, ID("complete_"), a.Run.ID, domain.CompleteRequest{
		Generation: a.Run.Generation, Status: "succeeded", Head: string(head),
		ArtifactID: artifact.ID, SessionID: "implementation", Quiescent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return f.candidate(t)
}

func TestMaintainabilityAdvisoryReachesReviewWithoutPublishing(t *testing.T) {
	f := newITFixture(t)
	qualityCandidate(t, f)
	if err := f.w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := f.candidate(t)
	if c.State != "prechecking" || !strings.Contains(c.Maintainability, codequality.Version) {
		t.Fatal("advisory signal lost before independent review", c)
	}
	main, err := f.g.Query(context.Background(), f.remote, "refs/heads/main")
	if err != nil || main != f.b {
		t.Fatal("unreviewed candidate moved main", main, err)
	}
	if err := f.w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.candidate(t).State != "reviewing" || f.snapshot(t).Tasks[0].Repairs != 0 {
		t.Fatal("advisory blocked review or consumed repair")
	}
	review := f.claim(t, "review")
	if review.Candidate == nil || review.Candidate.Maintainability != c.Maintainability {
		t.Fatal("reviewer lost fixed advisory context")
	}
}

func TestMaintainabilityAdvisoryDoesNotWaiveValidationEvidence(t *testing.T) {
	f := newITFixture(t)
	ctx := context.Background()
	qualityCandidate(t, f)
	for range 2 {
		if err := f.w.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	c := f.candidate(t)
	review := f.claim(t, "review")
	_, err := f.s.Complete(ctx, f.node, ID("review_"), review.Run.ID, domain.CompleteRequest{
		Generation: review.Run.Generation, Status: "succeeded", SessionID: "advisory-review", Quiescent: true,
		Review: testReview(review, domain.Review{Candidate: c.Commit, Verdict: "approve", Summary: "Cohesive change despite long literal", Findings: []string{}}),
	})
	if err != nil {
		t.Fatal(err)
	}
	c = f.candidate(t)
	if c.State != "ready_to_land" || c.CheckPlan == nil || c.CheckRunID == "" || c.Maintainability == "" {
		t.Fatal("real review/check pipeline did not retain advisory", c)
	}
	originalRun := c.CheckRunID
	c.CheckRunID = "missing-validation-run"
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error { return tx.Put("candidate", c.ID, c) }); err != nil {
		t.Fatal(err)
	}
	if err := f.w.land(ctx, c); err == nil {
		t.Fatal("advisory policy waived fixed test evidence")
	}
	main, err := f.g.Query(ctx, f.remote, "refs/heads/main")
	if err != nil || main != f.b || f.candidate(t).State != "ready_to_land" {
		t.Fatal("invalid evidence changed main/state", err)
	}
	c.CheckRunID = originalRun
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error { return tx.Put("candidate", c.ID, c) }); err != nil {
		t.Fatal(err)
	}
	if err := f.w.land(ctx, c); err != nil {
		t.Fatal(err)
	}
	main, err = f.g.Query(ctx, f.remote, "refs/heads/main")
	if err != nil || string(main) != c.Commit || f.candidate(t).State != "landed" {
		t.Fatal("valid reviewed candidate blocked by advisory", err)
	}
}

func TestPlanningMandatoryQualityPolicy(t *testing.T) {
	f, d, _ := planningFixture(t, true)
	scheduleTest(t, f, d, "quality-contract")
	w := &PlanningWorker{Service: f.s}
	run, document, messages, err := w.reserve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := w.planningPrompt(context.Background(), run, document, messages, t.TempDir())
	if err != nil || !strings.Contains(prompt, codequality.Instructions) {
		t.Fatal("planning contract omitted mandatory structural policy", err)
	}
	if !strings.Contains(requirementPlanPrompt([]byte("{}")), codequality.Instructions) {
		t.Fatal("requirement re-planning omitted structural policy")
	}
}
