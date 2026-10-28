package app

import (
	"context"
	"testing"

	"coordica/internal/store"
)

// Existing finite-budget scenarios explicitly opt in. Product defaults are
// tested separately, rather than silently retaining legacy fixture defaults.
func setTestTimeLimits(t *testing.T, service *Service, enabled bool) {
	t.Helper()
	if err := service.DB.Update(context.Background(), func(tx *store.Tx) error {
		settings, err := automation(tx)
		if err != nil {
			return err
		}
		settings.EnforceTimeLimits = enabled
		if enabled && settings.PlanningBudgetSeconds == 0 {
			settings.PlanningBudgetSeconds = 900
		}
		return tx.Put("automation", "current", settings)
	}); err != nil {
		t.Fatal(err)
	}
}
