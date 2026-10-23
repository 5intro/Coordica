package app

import (
	"context"
	"path/filepath"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func manualSignoffFixture(t *testing.T) (*itFixture, TestCatalog, domain.AcceptancePlan) {
	t.Helper()
	f := newITFixture(t)
	c, plan, _ := catalogDefinition(t, f)
	plan.Criteria[0].Method = "manual"
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		return tx.Put("acceptance_plan", plan.RequirementID, plan)
	}); err != nil {
		t.Fatal(err)
	}
	return f, c, plan
}

func TestAcceptanceSignoffHistoryIsBoundedAndSurvivesBackup(t *testing.T) {
	f, c, plan := manualSignoffFixture(t)
	s := AcceptanceSignoffs{Service: f.s}
	in := signoffRequest(t, f, c, plan)
	in.ManualCriteria = []string{"once"}
	for i := range 12 {
		in.ExpectedVersion = int64(i)
		in.Action, in.ExpectedDigest, in.ManualCriteria = "accept", catalogCoverage(t, c, f, plan).Digest, []string{"once"}
		if i%2 == 1 {
			in.Action, in.ExpectedDigest, in.ManualCriteria = "revoke", "", nil
		}
		if _, err := s.Decide(context.Background(), f.owner, ID("history_"), in); err != nil {
			t.Fatal(err)
		}
	}
	view := signoffStatus(t, f, plan.RequirementID)
	if view.Accepted || view.Receipt.Version != 12 || len(view.Recent) != 10 || view.Recent[9].Version != 3 {
		t.Fatal("bounded history is unordered or lost current revocation", view)
	}
	location, err := f.w.Backup(context.Background(), f.owner)
	if err != nil {
		t.Fatal(err)
	}
	records, err := readBackupRecords(context.Background(), filepath.Join(location, "coordica.db"), false)
	if err != nil || len(records.Signoffs) != 13 {
		t.Fatal("immutable declarations lost from backup", len(records.Signoffs), err)
	}
	oids, err := requiredBackupOIDs(records)
	if err != nil || len(oids) != 1 || oids[0] != f.b {
		t.Fatal("signoff-only Git commit omitted", oids, err)
	}
	destination := filepath.Join(t.TempDir(), "restored")
	if err := Restore(context.Background(), location, destination, f.g); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(destination, "coordica.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.View(context.Background(), func(tx *store.Tx) error {
		all, err := store.List[domain.AcceptanceSignoff](tx, "acceptance_signoff_revision")
		if err == nil && len(all) != 12 {
			t.Fatal("history was truncated in persistence", len(all))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
