package app

import (
	"context"
	"testing"

	"coordica/internal/store"
)

func TestQualityMigrationProtectsChecksAndDrainsV2Grants(t *testing.T) {
	f := newServiceFixture(t)
	run := f.claim(t, "old-quality-policy")
	before := f.snapshot(t)
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		return tx.Put("authority_policy_migration", "current", struct {
			Version int `json:"version"`
		}{2})
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.MigrateAuthorityPolicy(context.Background()); err != nil {
		t.Fatal(err)
	}
	after := f.snapshot(t)
	paths := []string{"AGENT.md", "QUALITY.md", "Makefile", "internal/codequality/**", "cmd/qualitycheck/**"}
	if !containsPaths(after.Project.ProtectedPaths, paths) || !after.Project.Paused {
		t.Fatal("quality policy unprotected or migration resumed execution")
	}
	if after.Initialization.ApprovedVersion != 0 || after.Tasks[0].ApprovedVersion != 0 {
		t.Fatal("old authority grandfathered into new gate")
	}
	if after.Runs[0].ID != run.Run.ID || after.Runs[0].Quiescent || after.Runs[0].State != "cancel_requested" {
		t.Fatal("migration fabricated a stopped process")
	}
	if after.Project.UsedSeconds != before.Project.UsedSeconds || after.Project.ReservedSeconds != before.Project.ReservedSeconds {
		t.Fatal("migration reset cumulative accounting")
	}
}

func TestQualityEntrypointMigrationUpdatesExistingV3Installation(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		p, err := getProject(tx)
		if err != nil {
			return err
		}
		p.ProtectedPaths = []string{"AGENTS.md", "scripts/check-quality.py"}
		if err = tx.Put("project", p.ID, p); err != nil {
			return err
		}
		return tx.Put("authority_policy_migration", "current", struct {
			Version int `json:"version"`
		}{3})
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.MigrateAuthorityPolicy(ctx); err != nil {
		t.Fatal(err)
	}
	first := f.snapshot(t)
	if !containsPaths(first.Project.ProtectedPaths, []string{"Makefile"}) || !first.Project.Paused {
		t.Fatal("v3 migration did not protect the quality entrypoint")
	}
	if err := f.s.MigrateAuthorityPolicy(ctx); err != nil {
		t.Fatal(err)
	}
	again := f.snapshot(t)
	if first.Project.PolicyVersion != again.Project.PolicyVersion || len(first.Events) != len(again.Events) {
		t.Fatal("migration replay changed policy or audit")
	}
}
