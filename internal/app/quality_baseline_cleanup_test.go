package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/gitops"
)

func qualityInvalidMain(t *testing.T, f *itFixture, source []byte) {
	t.Helper()
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "existing-project")
	if err := f.g.Clone(ctx, f.remote, dir); err != nil {
		t.Fatal(err)
	}
	if err := f.g.Checkout(ctx, dir, f.b); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "feature.go"), source, 0600); err != nil {
		t.Fatal(err)
	}
	head, err := f.g.CommitAll(ctx, dir, "existing invalid source", gitops.Identity{})
	if err != nil {
		t.Fatal(err)
	}
	itGit(t, f.g.GitPath, dir, "push", "--", f.remote, string(head)+":refs/heads/main")
	f.b = head
}

func TestInvalidMainSourceCanBeRepairedThroughPreparationAndLanding(t *testing.T) {
	for _, source := range [][]byte{[]byte("package p\nfunc broken(\n"), {0xff, 0x00}} {
		f := newITFixture(t)
		qualityInvalidMain(t, f, source)
		qualityCandidateSource(t, f, "src/feature.go", []byte("package p\nfunc work() {}\n"))
		ctx := context.Background()
		for range 2 {
			if err := f.w.Tick(ctx); err != nil {
				t.Fatal("cleanup failed preparation", err)
			}
		}
		if f.candidate(t).State != "reviewing" {
			t.Fatal("valid cleanup never reached independent review", f.candidate(t))
		}
		candidate := f.candidate(t)
		review := f.claim(t, "review")
		_, err := f.s.Complete(ctx, f.node, ID("review_"), review.Run.ID, domain.CompleteRequest{
			Generation: review.Run.Generation, Status: "succeeded", SessionID: "review-cleanup", Quiescent: true,
			Review: &domain.Review{Candidate: candidate.Commit, Verdict: "approve", Summary: "Fixed invalid source", Findings: []string{}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := f.w.Tick(ctx); err != nil {
			t.Fatal("cleanup failed landing", err)
		}
		if f.candidate(t).State != "landed" || f.snapshot(t).Tasks[0].Repairs != 0 {
			t.Fatal("cleanup was blocked or consumed an automatic repair", f.candidate(t))
		}
		main, err := f.g.Query(ctx, f.remote, "refs/heads/main")
		if err != nil || string(main) != candidate.Commit {
			t.Fatal("fixed candidate was not the exact landed main", main, err)
		}
	}
}
