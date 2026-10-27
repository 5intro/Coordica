package app

import (
	"context"

	"coordica/internal/domain"
	"coordica/internal/store"
)

// Read exposes the immutable source, diagnosis and follow-up to the Owner.
// Node chat and interaction profiles must not inherit the private source record.
func (c FailureRecoveries) Read(ctx context.Context, a Actor, id string) (domain.FailureAssessment, error) {
	var result domain.FailureAssessment
	if err := owner(a); err != nil {
		return result, err
	}
	if !domain.ChatText(id, 128, true) {
		return result, domain.Fail("invalid_input", "failure identity required")
	}
	err := c.Service.DB.View(ctx, func(tx *store.Tx) error { return tx.Get("failure_assessment", id, &result) })
	return result, err
}
