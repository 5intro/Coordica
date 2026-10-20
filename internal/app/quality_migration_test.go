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
	paths := []string{"AGENT.md", "QUALITY.md", "internal/codequality/**", "cmd/qualitycheck/**"}
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
