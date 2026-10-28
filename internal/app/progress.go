package app

import (
	"time"

	"coordica/internal/domain"
	"coordica/internal/store"
)

// Called only inside the owning, live heartbeat transaction. This stores the
// latest bounded observation and audit entry, never authority to cancel work.
func (s *Service) observeProgress(tx *store.Tx, run *domain.Run, observation *domain.ProgressObservation) error {
	if !domain.ProgressManaged(*run) {
		return nil
	}
	prior := domain.ProgressObservation{}
	checked := run.StartedAt
	if run.Progress != nil {
		prior = run.Progress.Observation
		checked = run.Progress.CheckedAt
	}
	if observation != nil {
		if err := domain.ValidateProgress(*observation); err != nil {
			return err
		}
		if observation.Sequence <= prior.Sequence {
			return nil // Retry or replay cannot refresh the observation's server time.
		}
		if observation.ToolStarted < prior.ToolStarted || observation.ToolCompleted < prior.ToolCompleted ||
			observation.Messages < prior.Messages || observation.Plans < prior.Plans {
			return domain.Fail("invalid_input", "progress counters cannot regress within a Run")
		}
	}
	interval := run.ProgressCheckSeconds
	if interval == 0 {
		interval = domain.DefaultProgressCheckSeconds
	}
	if s.Now().Before(checked.Add(time.Duration(interval) * time.Second)) {
		return nil
	}
	state := "needs_confirmation"
	if observation == nil {
		observation = &prior // Missing telemetry still produces a due check, never failure.
	}
	if observation.ToolStarted > prior.ToolStarted || observation.ToolCompleted > prior.ToolCompleted ||
		observation.Messages > prior.Messages || observation.Plans > prior.Plans {
		state = "activity_observed"
	}
	run.Progress = &domain.RunProgress{Observation: *observation, CheckedAt: s.Now(), State: state}
	return s.audit(tx, Actor{ID: "center"}, "run.progress_checked", run.ID, state)
}
