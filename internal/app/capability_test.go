package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestRegisteredToolIdentitySchedulingAndIndependentReview(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	b, err := f.s.CreateTask(ctx, f.owner, "custom-task", domain.Task{Title: "custom", Description: "custom protocol driver", Acceptance: "fixed candidate", AllowedPaths: []string{"src/**"}, Tool: "local-custom", BudgetSeconds: 7200, MaxRunSeconds: 3600})
	task := decodeTest[domain.Task](t, b, err)
	b, err = f.s.TaskAction(ctx, f.owner, "custom-approve", task.ID, "approve", domain.VersionRequest{ExpectedVersion: task.Version})
	task = decodeTest[domain.Task](t, b, err)
	caps := []domain.Capability{{Tool: "local-custom", Available: true, Structured: true, Cancel: true, Review: true}, {Tool: "independent-review", Available: true, Structured: true, Cancel: true, Review: true}}
	b, err = f.s.Claim(ctx, f.node, "custom-claim", domain.ClaimRequest{Capabilities: caps}, strings.Repeat("a", 40))
	first := decodeTest[*domain.Assignment](t, b, err)
	if first == nil || first.Run.Tool != "local-custom" {
		t.Fatalf("custom tool not scheduled: %+v", first)
	}
	artifact, err := f.s.UploadArtifact(ctx, f.node, first.Run.ID, strings.NewReader("immutable fixture"))
	if err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Second)
	if _, err = f.s.Complete(ctx, f.node, "custom-complete", first.Run.ID, domain.CompleteRequest{Generation: first.Run.Generation, Status: "succeeded", Head: strings.Repeat("b", 40), ArtifactID: artifact.ID, Quiescent: true}); err != nil {
		t.Fatal(err)
	}
	// The Git integrator normally records this fixed C after material verification.
	if err = f.s.DB.Update(ctx, func(tx *store.Tx) error {
		var current domain.Task
		if err := tx.Get("task", task.ID, &current); err != nil {
			return err
		}
		var candidate domain.Candidate
		if err := tx.Get("candidate", current.CurrentCandidateID, &candidate); err != nil {
			return err
		}
		candidate.Commit = strings.Repeat("c", 40)
		candidate.State = "reviewing"
		return tx.Put("candidate", candidate.ID, candidate)
	}); err != nil {
		t.Fatal(err)
	}
	noReview := append([]domain.Capability(nil), caps...)
	for index := range noReview {
		noReview[index].Review = false
	}
	unavailable, claimErr := f.s.Claim(ctx, f.node, "no-review-capability", domain.ClaimRequest{Capabilities: noReview}, "")
	if result := decodeTest[*domain.Assignment](t, unavailable, claimErr); result != nil {
		t.Fatal("omitted review capability granted a reviewer")
	}
	b, err = f.s.Claim(ctx, f.node, "custom-review", domain.ClaimRequest{Capabilities: caps}, "")
	review := decodeTest[*domain.Assignment](t, b, err)
	if review == nil || review.Run.Kind != "review" || review.Run.Tool != "independent-review" || review.Run.ID == first.Run.ID || review.Run.Base != strings.Repeat("c", 40) {
		t.Fatalf("fixed independent review was not dispatched: %+v", review)
	}
}

func TestSchedulingRequiresCurrentCapabilitiesAndRejectsMalformedReports(t *testing.T) {
	for _, capability := range []domain.Capability{
		{Tool: "codex", Available: true},
		{Tool: "codex", Available: true, Structured: true},
		{Tool: "codex", Available: false, Structured: true, Cancel: true},
		{Tool: "other", Available: true, Structured: true, Cancel: true},
	} {
		f := newServiceFixture(t)
		b, err := f.s.Claim(context.Background(), f.node, "no-capability", domain.ClaimRequest{Capabilities: []domain.Capability{capability}}, strings.Repeat("a", 40))
		if assignment := decodeTest[*domain.Assignment](t, b, err); assignment != nil {
			t.Fatalf("unsupported capability dispatched: %+v", capability)
		}
	}
	f := newServiceFixture(t)
	for _, caps := range [][]domain.Capability{{{Tool: "codex"}, {Tool: "codex"}}, {{Tool: "../tool"}}, make([]domain.Capability, 65)} {
		if _, err := f.s.Claim(context.Background(), f.node, ID("malformed"), domain.ClaimRequest{Capabilities: caps}, strings.Repeat("a", 40)); err == nil {
			t.Fatal("malformed report accepted")
		}
	}
	original := f.claim(t, "old-run")
	// Removal affects new dispatch only; it cannot authorize or replace an old Run.
	b, err := f.s.Claim(context.Background(), f.node, "removed", domain.ClaimRequest{}, strings.Repeat("a", 40))
	if next := decodeTest[*domain.Assignment](t, b, err); next != nil {
		t.Fatal("removed tool received new work")
	}
	if _, err = f.s.Heartbeat(context.Background(), f.node, original.Run.ID, domain.HeartbeatRequest{Generation: original.Run.Generation + 1}); err == nil {
		t.Fatal("capability refresh weakened generation fencing")
	}
	f.now = f.now.Add(f.s.Lease + time.Second)
	if err = f.s.Expire(context.Background()); err != nil {
		t.Fatal(err)
	}
	heartbeat, err := f.s.Heartbeat(context.Background(), f.node, original.Run.ID, domain.HeartbeatRequest{Generation: original.Run.Generation})
	if err != nil || !heartbeat.Cancel || heartbeat.Reason != "stale_run" {
		t.Fatalf("expired old lease accepted after capabilities changed: %+v, %v", heartbeat, err)
	}
}
