package app

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"coordica/internal/gitops"
)

func TestQualityLandingIOFailurePreservesReviewedCandidateAndRetries(t *testing.T) {
	f := newITFixture(t)
	f.implement(t, "src/feature.txt")
	f.reviewAndValidate(t)
	before := f.snapshot(t)
	candidate := f.candidate(t)
	f.w.Git = gitops.New(filepath.Join(t.TempDir(), "missing-git"))
	if err := f.w.Tick(context.Background()); err == nil {
		t.Fatal("unavailable Git scan allowed landing")
	}
	after := f.snapshot(t)
	if !reflect.DeepEqual(before.Candidates, after.Candidates) || !reflect.DeepEqual(before.Tasks, after.Tasks) {
		t.Fatal("operational scan error discarded review or consumed repair attempts", before.Tasks, after.Tasks)
	}
	if before.Project.UsedSeconds != after.Project.UsedSeconds || before.Project.ReservedSeconds != after.Project.ReservedSeconds {
		t.Fatal("scan failure changed cumulative accounting")
	}
	f.w.Git = f.g
	if err := f.w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	landed := f.candidate(t)
	if landed.ID != candidate.ID || landed.Commit != candidate.Commit || landed.State != "landed" {
		t.Fatal("retry failed to land the original reviewed candidate", landed)
	}
}

func TestQualityPreparationFailurePreservesCandidate(t *testing.T) {
	f := newITFixture(t)
	f.implement(t, "src/feature.txt")
	before := f.snapshot(t)
	c := f.candidate(t)
	f.w.Git = gitops.New(filepath.Join(t.TempDir(), "missing-git"))
	_, err := candidateCheckPlan(context.Background(), f.w, *before.Project, c)
	var scan *qualityScanFailure
	if !errors.As(err, &scan) {
		t.Fatal("scan failure lost its source", err)
	}
	if err = candidateCheckFailure(context.Background(), f.w, c, err); err == nil {
		t.Fatal("unavailable quality check silently passed")
	}
	after := f.snapshot(t)
	if !reflect.DeepEqual(before.Candidates, after.Candidates) || !reflect.DeepEqual(before.Tasks, after.Tasks) {
		t.Fatal("preparation error consumed a repair attempt")
	}
}
