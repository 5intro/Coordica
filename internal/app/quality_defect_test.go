package app

import (
	"context"
	"strings"
	"testing"

	"coordica/internal/codequality"
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
			main, err := f.g.Query(context.Background(), f.remote, "refs/heads/main")
			if err != nil || main != f.b {
				t.Fatal("analysis failure changed main", main, err)
			}
		})
	}
}
