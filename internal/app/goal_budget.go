package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func goalBudgetState(tx *store.Tx, d domain.PlanningDocument) (domain.GoalBudgetState, error) {
	state := domain.GoalBudgetState{GoalVersion: d.Version, GoalAuthorizationVersion: d.AuthorizationVersion,
		BudgetSeconds: d.BudgetSeconds, UsedSeconds: d.UsedSeconds, ReservedSeconds: d.ReservedSeconds}
	var saved domain.GoalBudgetState
	err := tx.Get("goal_budget_authorization", "current", &saved)
	if errors.Is(err, domain.ErrNotFound) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if saved.GoalVersion == d.Version && saved.GoalAuthorizationVersion == d.AuthorizationVersion {
		if saved.BudgetSeconds != d.BudgetSeconds {
			return state, domain.Fail("version_conflict", "goal allowance changed outside its authorization record")
		}
		state.Version, state.Reason = saved.Version, saved.Reason
	}
	return state, nil
}

func ReadGoalBudget(ctx context.Context, s *Service, a Actor) (domain.GoalBudgetState, error) {
	var state domain.GoalBudgetState
	if err := owner(a); err != nil {
		return state, err
	}
	err := s.DB.View(ctx, func(tx *store.Tx) error {
		d, err := goalDocument(tx)
		if err != nil {
			return err
		}
		state, err = goalBudgetState(tx, d)
		return err
	})
	return state, err
}

// Increasing an allowance does not extend an issued Run or revive any hold.
// The same transaction serializes this approval with reservations/settlement.
func IncreaseGoalBudget(ctx context.Context, s *Service, a Actor, key string, in domain.GoalBudgetRequest) (json.RawMessage, error) {
	if err := owner(a); err != nil {
		return nil, err
	}
	if in.ExpectedVersion < 0 || in.ExpectedGoalVersion < 1 || in.ExpectedAuthorizationVersion < 1 ||
		in.BudgetSeconds < 1 || in.BudgetSeconds > domain.MaxTaskBudgetSeconds || !domain.ChatText(in.Reason, 2048, true) {
		return nil, domain.Fail("invalid_input", "exact goal/allowance versions, finite budget and bounded reason required")
	}
	return s.mutate(ctx, a, key, "goal.budget.increase", in, func(tx *store.Tx) (any, error) {
		d, err := goalDocument(tx)
		if err != nil {
			return nil, err
		}
		if err = version(d.Version, in.ExpectedGoalVersion); err != nil {
			return nil, err
		}
		if err = version(d.AuthorizationVersion, in.ExpectedAuthorizationVersion); err != nil {
			return nil, err
		}
		state, err := goalBudgetState(tx, d)
		if err != nil {
			return nil, err
		}
		if err = version(state.Version, in.ExpectedVersion); err != nil {
			return nil, err
		}
		if d.Status != "admitted" || d.ApprovedVersion != d.Version || d.ApprovedAuthorizationVersion != d.AuthorizationVersion {
			return nil, domain.Fail("conflict", "a current authorized admitted goal is required")
		}
		if in.BudgetSeconds <= d.BudgetSeconds || in.BudgetSeconds < d.UsedSeconds+d.ReservedSeconds {
			return nil, domain.Fail("invalid_input", "new finite goal allowance must increase and cover all cumulative use/reservations")
		}
		before := state.BudgetSeconds
		d.BudgetSeconds, state.BudgetSeconds, state.Reason = in.BudgetSeconds, in.BudgetSeconds, in.Reason
		state.Version++
		if err = tx.Put("planning_document", "current", d); err != nil {
			return nil, err
		}
		if err = tx.Put("goal_budget_authorization", "current", state); err != nil {
			return nil, err
		}
		id := fmt.Sprintf("%d_%d_%d", d.Version, d.AuthorizationVersion, state.Version)
		if err = tx.Put("goal_budget_history", id, state); err != nil {
			return nil, err
		}
		return state, s.audit(tx, a, "goal.budget_increased", id, fmt.Sprintf("%d -> %d; %s", before, state.BudgetSeconds, in.Reason))
	})
}
