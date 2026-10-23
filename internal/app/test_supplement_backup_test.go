package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"coordica/internal/domain"
)

func TestSupplementOnlyCommitSurvivesBackupAndRestoreClosure(t *testing.T) {
	f := newITFixture(t)
	c, plan, _ := catalogDefinition(t, f)
	ctx := context.Background()
	if _, err := f.w.Base(ctx); err != nil {
		t.Fatal(err)
	}
	coverage := catalogCoverage(t, c, f, plan)
	if _, err := c.Supplement(ctx, f.owner, "backup-supplement", domain.TestSupplementRequest{
		RequirementID: plan.RequirementID, Commit: f.b.String(), ExpectedDigest: coverage.Digest}); err != nil {
		t.Fatal(err)
	}
	location, err := f.w.Backup(ctx, f.owner)
	if err != nil {
		t.Fatal(err)
	}
	records, err := readBackupRecords(ctx, filepath.Join(location, "coordica.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	oids, err := requiredBackupOIDs(records)
	if err != nil || len(oids) != 1 || oids[0] != f.b {
		t.Fatal("supplement-only commit omitted from backup closure", oids, err)
	}
	destination := filepath.Join(t.TempDir(), "restored")
	if err := Restore(ctx, location, destination, f.g); err != nil {
		t.Fatal(err)
	}
	itGit(t, f.g.GitPath, filepath.Join(destination, "repository"), "cat-file", "-e", f.b.String()+"^{commit}")
	data, err := os.ReadFile(filepath.Join(location, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest backupManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Git = nil
	data, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(location, "manifest.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := RestoreBackup(ctx, location, filepath.Join(t.TempDir(), "missing-git")); err == nil {
		t.Fatal("restore accepted a supplement without its required Git object")
	}
}
