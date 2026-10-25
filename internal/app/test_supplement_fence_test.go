package app

import (
	"context"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestSupplementTaskFencesCurrentCoverageInputs(t *testing.T) {
	for _, mode := range []string{"unchanged", "case", "suite", "new_result", "cycle"} {
		t.Run(mode, func(t *testing.T) {
			f := newITFixture(t)
			c, plan, v := catalogDefinition(t, f)
			coverage := catalogCoverage(t, c, f, plan)
			raw, err := c.Supplement(context.Background(), f.owner, "supplement-fence", domain.TestSupplementRequest{
				RequirementID: plan.RequirementID, Commit: f.b.String(), ExpectedDigest: coverage.Digest})
			supplement := itDecode[domain.TestSupplementPlan](t, raw, err)
			if mode == "new_result" || mode == "cycle" {
				seedCatalogEvidence(t, f, v)
			}
			if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
				switch mode {
				case "case":
					v.Version++
					v.Steps = []string{"Changed implementation instructions"}
					return tx.Put("test_case", v.ID, v)
				case "suite":
					var suite domain.TestSuite
					if err := tx.Get("test_suite", v.SuiteID, &suite); err != nil {
						return err
					}
					suite.Version++
					return tx.Put("test_suite", suite.ID, suite)
				case "cycle":
					var run domain.Run
					if err := tx.Get("run", "accepted_test", &run); err != nil {
						return err
					}
					var task domain.Task
					if err := tx.Get("task", run.TaskID, &task); err != nil {
						return err
					}
					task.RequirementIDs = []string{supplement.RequirementID}
					return tx.Put("task", task.ID, task)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := f.s.DB.View(context.Background(), func(tx *store.Tx) error {
				err := requirementTaskGate(tx, domain.Task{RequirementIDs: []string{supplement.RequirementID}})
				if (err == nil) != (mode == "unchanged") {
					t.Fatal("supplement task ignored its fixed coverage inputs", mode, err)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
