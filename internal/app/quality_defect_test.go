package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"coordica/internal/codequality"
	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/store"
)

func TestDeterministicCandidateAnalysisFailureQueuesOneBoundedRepair(t *testing.T) {
	for _, fixture := range []struct {
		name, path, text string
	}{
		{"malformed Go", "src/feature.go", "package p\nfunc broken(\n"},
		{"non UTF-8", "src/feature.js", string([]byte{0xff, 0xfe})},
		{"oversized blob", "src/feature.js", strings.Repeat("x", codequality.MaxFileBytes+1)},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			f := newITFixture(t)
			qualityCandidateSource(t, f, fixture.path, []byte(fixture.text))
			before := f.snapshot(t)
			if err := f.w.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			after := f.snapshot(t)
			if f.candidate(t).State != "rejected" || after.Tasks[0].State != "ready" || after.Tasks[0].Repairs != before.Tasks[0].Repairs+1 {
				t.Fatal("immutable source defect was retried forever instead of queued for repair", after.Tasks, after.Candidates)
			}
			if before.Project.UsedSeconds != after.Project.UsedSeconds || before.Project.ReservedSeconds != after.Project.ReservedSeconds {
				t.Fatal("analysis rejection reset cumulative accounting")
			}
			if err := f.w.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			if f.snapshot(t).Tasks[0].Repairs != after.Tasks[0].Repairs {
				t.Fatal("rejected candidate consumed another repair")
			}
			candidate := f.candidate(t)
			if candidate.Commit == "" || candidate.Main != string(f.b) {
				t.Fatal("rejection lost the built repair input", candidate)
			}
			repair := f.claim(t, "implement")
			if repair.Run.Base != candidate.Commit || repair.Candidate == nil || repair.Candidate.ID != candidate.ID {
				t.Fatal("repair did not receive its defective fixed candidate", repair)
			}
			bundle, err := f.w.InputBundle(context.Background(), repair.Run)
			if err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(t.TempDir(), "repair-input")
			if err := f.g.CloneSnapshot(context.Background(), bundle, gitops.OID(candidate.Commit), dir); err != nil {
				t.Fatal(err)
			}
			text, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(fixture.path)))
			if err != nil || string(text) != fixture.text {
				t.Fatal("repair input bundle lost the defect that must be fixed", err)
			}
			main, err := f.g.Query(context.Background(), f.remote, "refs/heads/main")
			if err != nil || main != f.b {
				t.Fatal("analysis failure changed main", main, err)
			}
		})
	}
}

func TestStaleBuiltCandidateCannotReplaceNewerRejectionInput(t *testing.T) {
	f := newITFixture(t)
	c := qualityCandidate(t, f)
	c.Main, c.Commit = string(f.b), c.Head
	ctx := context.Background()
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		var current domain.Candidate
		if err := tx.Get("candidate", c.ID, &current); err != nil {
			return err
		}
		current.Version++
		return tx.Put("candidate", current.ID, current)
	}); err != nil {
		t.Fatal(err)
	}
	before := f.snapshot(t)
	err := f.w.reject(ctx, c, errors.New("stale source defect"), false)
	var failure *domain.Error
	if !errors.As(err, &failure) || failure.Code != "candidate_changed" {
		t.Fatal("stale build was not fenced", err)
	}
	current := f.candidate(t)
	if current.Commit != "" || current.State != "accepted" || f.snapshot(t).Tasks[0].Repairs != before.Tasks[0].Repairs {
		t.Fatal("stale rejection changed candidate or repair allowance", current)
	}
}
