package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func signoffFixture(t *testing.T) (*itFixture, TestCatalog, domain.AcceptancePlan, domain.TestCase) {
	t.Helper()
	f := newITFixture(t)
	c, plan, v := catalogDefinition(t, f)
	plan.Criteria = append(plan.Criteria, domain.AcceptanceCriterion{ID: "experience", Title: "Payment feedback is clear", Method: "manual"})
	raw, err := c.SaveAcceptance(context.Background(), f.owner, "signoff-plan", domain.AcceptanceSave{
		ExpectedRequirementVersion: 1, ExpectedVersion: plan.Version, Plan: plan})
	plan = itDecode[domain.AcceptancePlan](t, raw, err)
	v.Coverage[0].PlanVersion = plan.Version
	raw, err = c.Save(context.Background(), f.owner, "signoff-case", domain.TestCaseSave{ExpectedVersion: v.Version, Case: v})
	v = itDecode[domain.TestCase](t, raw, err)
	seedCatalogEvidence(t, f, v)
	return f, c, plan, v
}

func signoffRequest(t *testing.T, f *itFixture, c TestCatalog, plan domain.AcceptancePlan) domain.AcceptanceSignoffRequest {
	t.Helper()
	coverage := catalogCoverage(t, c, f, plan)
	return domain.AcceptanceSignoffRequest{RequirementID: plan.RequirementID, Commit: f.b.String(), Action: "accept",
		ExpectedDigest: coverage.Digest, ManualCriteria: []string{"experience"}, Note: "Retried the payment; feedback is clear."}
}

func signoffStatus(t *testing.T, f *itFixture, id string) domain.AcceptanceSignoffStatus {
	t.Helper()
	out, err := (AcceptanceSignoffs{Service: f.s}).Read(context.Background(), f.owner, id, f.b.String())
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestAcceptanceSignoffReplayRevocationAndOfflineRecovery(t *testing.T) {
	f, c, plan, _ := signoffFixture(t)
	s := AcceptanceSignoffs{Service: f.s}
	ctx := context.Background()
	in := signoffRequest(t, f, c, plan)
	in.Commit = strings.ToUpper(in.Commit)
	for _, actor := range []Actor{f.node, {ID: "limited", Role: "chat"}} {
		if _, err := s.Decide(ctx, actor, "forbidden", in); err == nil {
			t.Fatal("limited actor signed acceptance")
		}
		if _, err := s.Read(ctx, actor, plan.RequirementID, in.Commit); err == nil {
			t.Fatal("limited actor read private acceptance")
		}
	}
	raw, err := s.Decide(ctx, f.owner, "accept-once", in)
	receipt := itDecode[domain.AcceptanceSignoff](t, raw, err)
	if receipt.Commit != f.b.String() || receipt.Version != 1 || !signoffStatus(t, f, plan.RequirementID).Accepted {
		t.Fatal("fixed declaration was not accepted", receipt)
	}
	if catalogCoverage(t, c, f, plan).Verified || !f.snapshot(t).Project.Paused {
		t.Fatal("human signoff fabricated automatic test success or resumed execution")
	}
	repo := filepath.Join(f.s.Dir, "repository")
	if err := os.Rename(repo, repo+"-offline"); err != nil {
		t.Fatal(err)
	}
	replay, err := s.Decide(ctx, f.owner, "accept-once", in)
	if err != nil || string(replay) != string(raw) {
		t.Fatal("accepted receipt could not replay offline", err)
	}
	changed := in
	changed.Note = "This is a different declaration."
	if _, err := s.Decide(ctx, f.owner, "accept-once", changed); err == nil {
		t.Fatal("replay key accepted a changed declaration")
	}
	revoke := domain.AcceptanceSignoffRequest{RequirementID: plan.RequirementID, Commit: f.b.String(),
		Action: "revoke", ExpectedVersion: 1, Note: "Business acceptance needs another check."}
	rawRevoked, err := s.Decide(ctx, f.owner, "revoke-once", revoke)
	value := itDecode[domain.AcceptanceSignoff](t, rawRevoked, err)
	if value.State != "revoked" || value.Version != 2 {
		t.Fatal(value)
	}
	if _, err := s.Decide(ctx, f.owner, "stale-revoke", revoke); err == nil {
		t.Fatal("stale revocation changed a newer receipt")
	}
	if _, err := s.Decide(ctx, f.owner, "accept-once", in); err != nil {
		t.Fatal("original accepted receipt was lost", err)
	}
	view := signoffStatus(t, f, plan.RequirementID)
	if view.Accepted || len(view.Recent) != 2 || view.Recent[1].Note != in.Note || view.Receipt.State != "revoked" {
		t.Fatal("replay resurrected acceptance or erased history", view)
	}
}

func TestAcceptanceSignoffConcurrentDeclarationsHaveOneWinner(t *testing.T) {
	f, c, plan, _ := signoffFixture(t)
	s := AcceptanceSignoffs{Service: f.s}
	in := signoffRequest(t, f, c, plan)
	var winners atomic.Int32
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Decide(context.Background(), f.owner, ID("concurrent_"), in); err == nil {
				winners.Add(1)
			}
		}()
	}
	wg.Wait()
	view := signoffStatus(t, f, plan.RequirementID)
	if winners.Load() != 1 || !view.Accepted || len(view.Recent) != 1 || view.Receipt.Version != 1 {
		t.Fatal("concurrent writes escaped the version fence", winners.Load(), view)
	}
}

func TestAcceptanceSignoffRejectsIncompleteOrChangedConfirmation(t *testing.T) {
	f, c, plan, _ := signoffFixture(t)
	s := AcceptanceSignoffs{Service: f.s}
	for _, mode := range []string{"missing_manual", "extra_manual", "duplicate_manual", "blank_note", "old_digest", "missing_commit"} {
		in := signoffRequest(t, f, c, plan)
		switch mode {
		case "missing_manual":
			in.ManualCriteria = nil
		case "extra_manual":
			in.ManualCriteria = append(in.ManualCriteria, "once")
		case "duplicate_manual":
			in.ManualCriteria = append(in.ManualCriteria, "experience")
		case "blank_note":
			in.Note = " "
		case "old_digest":
			in.ExpectedDigest = strings.Repeat("a", 64)
		case "missing_commit":
			in.Commit = strings.Repeat("e", 40)
		}
		if _, err := s.Decide(context.Background(), f.owner, mode, in); err == nil {
			t.Fatal("invalid confirmation accepted", mode)
		}
	}
	if signoffStatus(t, f, plan.RequirementID).Receipt != nil {
		t.Fatal("rejected confirmation left a receipt")
	}
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		var run domain.Run
		if err := tx.Get("run", "accepted_test", &run); err != nil {
			return err
		}
		run.State = "stopped"
		return tx.Put("run", run.ID, run)
	}); err != nil {
		t.Fatal(err)
	}
	in := signoffRequest(t, f, c, plan)
	if _, err := s.Decide(context.Background(), f.owner, "unverified", in); err == nil {
		t.Fatal("Owner signoff bypassed missing automated evidence")
	}
}

func TestAcceptanceSignoffDoesNotSurviveChangedEvidence(t *testing.T) {
	for _, mode := range []string{"case_changed", "clause_changed", "requirement_changed", "unquiet", "new_attempt", "withdrawn", "metadata"} {
		t.Run(mode, func(t *testing.T) {
			f, c, plan, v := signoffFixture(t)
			s := AcceptanceSignoffs{Service: f.s}
			in := signoffRequest(t, f, c, plan)
			if _, err := s.Decide(context.Background(), f.owner, "initial", in); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "new_attempt":
				var suite domain.TestSuite
				_ = f.s.DB.View(context.Background(), func(tx *store.Tx) error { return tx.Get("test_suite", v.SuiteID, &suite) })
				requestVerification(t, f, suite)
			case "withdrawn", "metadata":
				if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
					var r domain.Requirement
					if err := tx.Get("requirement", plan.RequirementID, &r); err != nil {
						return err
					}
					r.Version++
					if mode == "withdrawn" {
						r.State = "withdrawn"
					}
					return tx.Put("requirement", r.ID, r)
				}); err != nil {
					t.Fatal(err)
				}
			default:
				mutateCatalogEvidence(t, f, domain.Task{}, plan, v, mode)
			}
			view := signoffStatus(t, f, plan.RequirementID)
			if view.Accepted != (mode == "metadata") || view.Receipt.State != "accepted" {
				t.Fatal("stale acceptance became current, or history was rewritten", view)
			}
			in.ExpectedVersion = 1
			if mode != "metadata" {
				if _, err := s.Decide(context.Background(), f.owner, "old-preview", in); err == nil {
					t.Fatal("changed preview was signed")
				}
				_, err := s.Decide(context.Background(), f.owner, "revoke-outdated", domain.AcceptanceSignoffRequest{
					RequirementID: plan.RequirementID, Commit: f.b.String(), ExpectedVersion: 1, Action: "revoke", Note: "Outdated."})
				if err != nil {
					t.Fatal("outdated declaration could not be revoked", err)
				}
			}
		})
	}
}

func TestAcceptanceSignoffAutomatedEvidenceDoesNotSelfAccept(t *testing.T) {
	f := newITFixture(t)
	c, plan, v := catalogDefinition(t, f)
	seedCatalogEvidence(t, f, v)
	s := AcceptanceSignoffs{Service: f.s}
	view := signoffStatus(t, f, plan.RequirementID)
	if !view.CanAccept || view.Accepted || !catalogCoverage(t, c, f, plan).Verified {
		t.Fatal("automated verification was confused with human acceptance", view)
	}
	in := signoffRequest(t, f, c, plan)
	in.ManualCriteria = nil
	if _, err := s.Decide(context.Background(), f.owner, "automated-owner-signoff", in); err != nil {
		t.Fatal(err)
	}
	if !signoffStatus(t, f, plan.RequirementID).Accepted || !catalogCoverage(t, c, f, plan).Verified {
		t.Fatal("explicit acceptance changed test evidence")
	}
}
