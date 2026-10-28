package app

import (
	"errors"
	"time"

	"coordica/internal/domain"
	"coordica/internal/store"
)

// Missing policy is the new telemetry-only default, including legacy settings.
// Unexpected persistence failures conservatively deny budget-dependent work.
func cumulativeLimit(tx *store.Tx, budget, used, reserved int64) bool {
	var settings domain.AutomationSettings
	err := tx.Get("automation", "current", &settings)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return true
	}
	return settings.EnforceTimeLimits && budget > 0 && used+reserved >= budget
}

func taskTimeExhausted(tx *store.Tx, task domain.Task) bool {
	return cumulativeLimit(tx, task.BudgetSeconds, task.UsedSeconds, task.ReservedSeconds)
}

func taskTimeSpent(tx *store.Tx, task domain.Task) bool {
	// An existing Run owns its reservation. Filling the allowance with reserved
	// time must deny new work, not revoke the execution that reserved it.
	return cumulativeLimit(tx, task.BudgetSeconds, task.UsedSeconds, 0)
}

func goalTimeExhausted(tx *store.Tx, goal domain.PlanningDocument) bool {
	return cumulativeLimit(tx, goal.BudgetSeconds, goal.UsedSeconds, goal.ReservedSeconds)
}

func executionGrant(tx *store.Tx, task domain.Task, kind string) (int64, bool, error) {
	settings, err := automation(tx)
	if err != nil {
		return 0, false, err
	}
	if kind == "implement" && !settings.EnforceTimeLimits {
		return 0, true, nil
	}
	grant := task.MaxRunSeconds
	if grant == 0 {
		grant = 3600 // Bounded review/program operation, not a development deadline.
	}
	if settings.EnforceTimeLimits {
		if task.BudgetSeconds > 0 {
			grant = limit(grant, task.BudgetSeconds-task.UsedSeconds-task.ReservedSeconds)
		}
		if task.GoalVersion != 0 {
			var goal domain.PlanningDocument
			if err = tx.Get("planning_document", "current", &goal); err != nil {
				return 0, false, err
			}
			if goal.BudgetSeconds > 0 {
				grant = limit(grant, goal.BudgetSeconds-goal.UsedSeconds-goal.ReservedSeconds)
			}
		}
	}
	return grant, false, nil
}

func planningGrant(settings domain.AutomationSettings, goal domain.PlanningDocument) int64 {
	grant := settings.PlanningTimeoutSeconds
	if settings.EnforceTimeLimits {
		if settings.PlanningBudgetSeconds > 0 {
			grant = limit(grant, settings.PlanningBudgetSeconds-goal.PlanningUsedSeconds-goal.PlanningReservedSeconds)
		}
		if goal.BudgetSeconds > 0 {
			grant = limit(grant, goal.BudgetSeconds-goal.UsedSeconds-goal.ReservedSeconds)
		}
	}
	return grant
}

func runRemainingReservation(run domain.Run) int64 {
	if domain.ProgressManaged(run) {
		return 0
	}
	return run.GrantSeconds - run.ElapsedSeconds
}

func settleRunGoal(tx *store.Tx, task domain.Task, run domain.Run, delta, released int64) error {
	if !domain.ProgressManaged(run) {
		return settleGoal(tx, task, delta, released)
	}
	// Progress-managed execution reserves no future lifetime. Record only the
	// newly observed use; never debit somebody else's finite reservation.
	if task.GoalVersion == 0 {
		return nil
	}
	var goal domain.PlanningDocument
	if err := tx.Get("planning_document", "current", &goal); err != nil {
		return err
	}
	goal.UsedSeconds += delta
	return tx.Put("planning_document", "current", goal)
}

func limitLease(run *domain.Run) {
	if !run.Deadline.IsZero() && run.Deadline.Before(run.LeaseUntil) {
		run.LeaseUntil = run.Deadline
	}
}

func runElapsedAllowed(run domain.Run, elapsed int64, now time.Time) bool {
	return elapsed >= 0 && domain.RunTimeValid(run, now) && (domain.ProgressManaged(run) || elapsed <= run.GrantSeconds)
}

// settle only debits the delta. A lost acknowledgement cannot double-charge.
func (s *Service) settle(p *domain.Project, t *domain.Task, r *domain.Run, elapsed int64, finish bool) {
	until := s.Now()
	if domain.ProgressManaged(*r) && until.After(r.LeaseUntil) {
		until = r.LeaseUntil
	}
	duration := until.Sub(r.StartedAt)
	wall := int64(duration / time.Second)
	if duration%time.Second > 0 {
		wall++
	} // Sub-second activity is not free.
	if elapsed < wall {
		elapsed = wall
	}
	if domain.ProgressManaged(*r) {
		elapsed = wall // Unbounded lifetime must not accept unbounded client charges.
	}
	if elapsed < r.ElapsedSeconds {
		elapsed = r.ElapsedSeconds
	}
	if !domain.ProgressManaged(*r) && elapsed > r.GrantSeconds {
		elapsed = r.GrantSeconds
	}
	delta := elapsed - r.ElapsedSeconds
	t.UsedSeconds += delta
	p.UsedSeconds += delta
	if !domain.ProgressManaged(*r) {
		t.ReservedSeconds -= delta
		p.ReservedSeconds -= delta
	}
	r.ElapsedSeconds = elapsed
	if finish && !domain.ProgressManaged(*r) {
		remaining := r.GrantSeconds - r.ElapsedSeconds
		t.ReservedSeconds -= remaining
		p.ReservedSeconds -= remaining
	}
	t.Version++
	p.Version++
	r.Version++
}
