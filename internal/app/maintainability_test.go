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

func TestMaintainabilityRejectsBeforeReviewWithoutPublishing(t *testing.T) {
	f := newITFixture(t)
	qualityCandidate(t, f)
	if err := f.w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := f.candidate(t)
	if c.State != "rejected" || !strings.Contains(c.Detail, "maintainability-v1") {
		t.Fatal("invalid structure reached review", c)
	}
	main, err := f.g.Query(context.Background(), f.remote, "refs/heads/main")
	if err != nil || main != f.b {
		t.Fatal("rejected candidate moved main", main, err)
	}
}

func TestMaintainabilityBlocksLegacyApprovedCandidateAtLanding(t *testing.T) {
	f := newITFixture(t)
	ctx := context.Background()
	c := qualityCandidate(t, f)
	// Let prepare import the actual artifact; structural rejection must not
	// erase those fixed objects. Then simulate a pre-upgrade approved record.
	if err := f.w.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	c = f.candidate(t)
	fixed, err := f.g.BuildCandidate(ctx, f.w.repo(), f.b, gitops.OID(c.Head),
		filepath.Join(t.TempDir(), "legacy"), gitops.Identity{})
	if err != nil {
		t.Fatal(err)
	}
	c.Main, c.Commit = string(f.b), string(fixed.Commit)
	c.State, c.ApprovedCommit = "ready_to_land", c.Commit
	c.Protected = true
	c.Review = &domain.Review{Candidate: c.Commit, Verdict: "approve"}
	c.Checks = []domain.CheckResult{{Name: "clean_git", Passed: true}}
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		var task domain.Task
		if err := tx.Get("task", c.TaskID, &task); err != nil {
			return err
		}
		task.State, task.CurrentCandidateID, task.HoldReason = "awaiting_integration", c.ID, ""
		if err := tx.Put("task", task.ID, task); err != nil {
			return err
		}
		return tx.Put("candidate", c.ID, c)
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.w.land(ctx, c); err != nil {
		t.Fatal(err)
	}
	if f.candidate(t).State != "rejected" {
		t.Fatal("legacy approval bypassed structural gate")
	}
	main, err := f.g.Query(ctx, f.remote, "refs/heads/main")
	if err != nil || main != f.b {
		t.Fatal("bad legacy candidate published", main, err)
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
