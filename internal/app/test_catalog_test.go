package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func catalogDefinition(t *testing.T, f *itFixture) (TestCatalog, domain.AcceptancePlan, domain.TestCase) {
	t.Helper()
	c := TestCatalog{Service: f.s}
	if _, err := f.w.Base(context.Background()); err != nil {
		t.Fatal(err)
	}
	p, err := f.s.Project(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.ProjectAction(context.Background(), f.owner, "catalog-pause", "pause",
		domain.VersionRequest{ExpectedVersion: p.Version}); err != nil {
		t.Fatal(err)
	}
	raw, err := f.s.CreateRequirement(context.Background(), f.owner, "catalog-source", domain.Requirement{Title: "Payments", Body: "Pay once"})
	r := itDecode[domain.Requirement](t, raw, err)
	in := domain.AcceptanceSave{ExpectedRequirementVersion: r.Version, Plan: domain.AcceptancePlan{
		RequirementID: r.ID, Revision: 1, Criteria: []domain.AcceptanceCriterion{{ID: "once", Title: "Retries pay once", Method: "automated"}}}}
	raw, err = c.SaveAcceptance(context.Background(), f.owner, "catalog-acceptance", in)
	plan := itDecode[domain.AcceptancePlan](t, raw, err)
	suite := testSuite(t, f.s, f.owner, "project")
	v := domain.TestCase{ID: "pay_once", Name: "Payment retries", Purpose: "Prevent double charge", Enabled: true, Implemented: true,
		Categories: []string{"black_box", "regression"}, Steps: []string{"Retry payment"}, Assertions: []string{"One charge"},
		SuiteID: suite.ID, CheckName: "clean", ReportFormat: "go-json", ExternalID: "p#TestPay",
		Coverage: []domain.CriterionRef{{RequirementID: r.ID, Revision: 1, PlanVersion: plan.Version, CriterionID: "once"}}}
	raw, err = c.Save(context.Background(), f.owner, "catalog-case", domain.TestCaseSave{Case: v})
	return c, plan, itDecode[domain.TestCase](t, raw, err)
}

func catalogCoverage(t *testing.T, c TestCatalog, f *itFixture, plan domain.AcceptancePlan) domain.AcceptanceCoverage {
	t.Helper()
	result, err := c.Coverage(context.Background(), f.owner, plan.RequirementID, f.b.String())
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestCatalogAuthorizationReplayAndVersionedDefinitions(t *testing.T) {
	f := newITFixture(t)
	c, plan, v := catalogDefinition(t, f)
	ctx := context.Background()
	if _, err := c.Save(ctx, f.node, "forbidden", domain.TestCaseSave{Case: v}); err == nil {
		t.Fatal("node edited catalog")
	}
	if _, err := c.Coverage(ctx, f.node, plan.RequirementID, f.b.String()); err == nil {
		t.Fatal("node read Owner coverage")
	}
	in := domain.TestCaseSave{ExpectedVersion: v.Version, Case: v}
	in.Case.Name = "New name"
	raw, err := c.Save(ctx, f.owner, "case-edit", in)
	v = itDecode[domain.TestCase](t, raw, err)
	replay, err := c.Save(ctx, f.owner, "case-edit", in)
	if err != nil || string(raw) != string(replay) || v.Version != 2 {
		t.Fatal("lost replay identity", err)
	}
	if _, err := c.Save(ctx, f.owner, "stale-edit", in); err == nil {
		t.Fatal("stale edit admitted")
	}
	if err := f.s.DB.View(ctx, func(tx *store.Tx) error {
		var old domain.TestCase
		if err := tx.Get("test_case_revision", "pay_once_v1", &old); err != nil {
			return err
		}
		if old.Name != "Payment retries" {
			t.Fatal("old case changed")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	page, err := c.List(ctx, f.owner, domain.TestCaseQuery{Category: "white_box", Limit: 1})
	if err != nil || len(page.Items) != 0 {
		t.Fatal(page, err)
	}
}

func TestCatalogSupplementAtomicDedupAndStalePreview(t *testing.T) {
	f := newITFixture(t)
	c, plan, _ := catalogDefinition(t, f)
	coverage := catalogCoverage(t, c, f, plan)
	in := domain.TestSupplementRequest{RequirementID: plan.RequirementID, Commit: f.b.String(), ExpectedDigest: coverage.Digest}
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			raw, err := c.Supplement(context.Background(), f.owner, ID("supplement_"), in)
			if err != nil {
				t.Error(err)
				return
			}
			var result domain.TestSupplementPlan
			if err := json.Unmarshal(raw, &result); err != nil {
				t.Error(err)
			}
			ids <- result.RequirementID
		}()
	}
	wg.Wait()
	close(ids)
	id := ""
	for got := range ids {
		if id != "" && got != id {
			t.Fatal("duplicate supplemental work")
		}
		id = got
	}
	if id == "" {
		t.Fatal("supplement missing")
	}
	snap := f.snapshot(t)
	if !snap.Project.Paused || len(snap.PlanningRuns) != 0 || len(snap.Requirements) != 2 {
		t.Fatal("supplement changed scheduling authority")
	}
	for _, req := range snap.Requirements {
		if req.ID == id && (!strings.Contains(req.Body, "run_test") || len(req.RelatedIDs) != 1) {
			t.Fatal("missing explainable gap or source")
		}
	}
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		plan.Version++
		return tx.Put("acceptance_plan", plan.RequirementID, plan)
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DB.View(context.Background(), func(tx *store.Tx) error {
		if err := requirementTaskGate(tx, domain.Task{RequirementIDs: []string{id}}); err == nil {
			t.Fatal("stale supplemental task authorized")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	in.ExpectedDigest = strings.Repeat("a", 64)
	if _, err := c.Supplement(context.Background(), f.owner, "stale-preview", in); err == nil {
		t.Fatal("stale preview created work")
	}
}

func TestCatalogSupplementPreviewIsStableAcrossMetadataLinks(t *testing.T) {
	f := newITFixture(t)
	c, plan, _ := catalogDefinition(t, f)
	before := catalogCoverage(t, c, f, plan)
	in := domain.TestSupplementRequest{RequirementID: plan.RequirementID, Commit: f.b.String(), ExpectedDigest: before.Digest}
	raw, err := c.Supplement(context.Background(), f.owner, "first-supplement", in)
	first := itDecode[domain.TestSupplementPlan](t, raw, err)
	after := catalogCoverage(t, c, f, plan)
	if before.Digest != after.Digest {
		t.Fatal("metadata changed coverage identity")
	}
	in.ExpectedDigest = after.Digest
	raw, err = c.Supplement(context.Background(), f.owner, "after-refresh", in)
	second := itDecode[domain.TestSupplementPlan](t, raw, err)
	if first.RequirementID != second.RequirementID || len(f.snapshot(t).Requirements) != 2 {
		t.Fatal("refresh duplicated supplementary work")
	}
}

func TestCatalogManualAcceptanceDoesNotBecomeAutomaticWork(t *testing.T) {
	f := newITFixture(t)
	c, plan, _ := catalogDefinition(t, f)
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		plan.Criteria[0].Method = "manual"
		return tx.Put("acceptance_plan", plan.RequirementID, plan)
	}); err != nil {
		t.Fatal(err)
	}
	coverage := catalogCoverage(t, c, f, plan)
	if _, err := c.Supplement(context.Background(), f.owner, "manual-only", domain.TestSupplementRequest{
		RequirementID: plan.RequirementID, Commit: f.b.String(), ExpectedDigest: coverage.Digest,
	}); err == nil || len(f.snapshot(t).Requirements) != 1 {
		t.Fatal("manual acceptance created automatic developer work")
	}
}

func TestCatalogOversizedSupplementRollsBackAndPaginationIsBounded(t *testing.T) {
	f := newITFixture(t)
	c, plan, v := catalogDefinition(t, f)
	for _, id := range []string{"pay_two", "pay_three"} {
		v.ID, v.Version = id, 0
		v.Implemented = false
		v.Steps = []string{strings.Repeat("x", 2048)}
		v.Assertions = []string{strings.Repeat("x", 2048)}
		_, err := c.Save(context.Background(), f.owner, id, domain.TestCaseSave{Case: v})
		if err != nil {
			t.Fatal(err)
		}
	}
	first, err := c.List(context.Background(), f.owner, domain.TestCaseQuery{Limit: 2, Category: "regression"})
	if err != nil || len(first.Items) != 2 || first.Next == "" {
		t.Fatal(first, err)
	}
	last, err := c.List(context.Background(), f.owner, domain.TestCaseQuery{Limit: 2, Category: "regression", After: first.Next})
	if err != nil || len(last.Items) != 1 || last.Items[0].ID <= first.Next || last.Next != "" {
		t.Fatal(last, err)
	}
	v.ID, v.Version = "pay_large", 0
	v.Steps = make([]string, 32)
	for i := range v.Steps {
		v.Steps[i] = strings.Repeat("x", 2048)
	}
	if _, err := c.Save(context.Background(), f.owner, "large-case", domain.TestCaseSave{Case: v}); err != nil {
		t.Fatal(err)
	}
	coverage := catalogCoverage(t, c, f, plan)
	_, err = c.Supplement(context.Background(), f.owner, "large-plan", domain.TestSupplementRequest{
		RequirementID: plan.RequirementID, Commit: f.b.String(), ExpectedDigest: coverage.Digest,
	})
	if err == nil || len(f.snapshot(t).Requirements) != 1 {
		t.Fatal("oversized plan partially created work")
	}
}
