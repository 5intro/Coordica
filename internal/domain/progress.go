package domain

import "time"

const ProgressVersion = 1
const DefaultProgressCheckSeconds int64 = 1800
const MaxProgressCheckSeconds int64 = 86400

// ProgressObservation contains activity counters, not model claims of success.
// It is cumulative for one Run; missing replies never attest a failed process.
type ProgressObservation struct {
	Sequence      int64 `json:"sequence"`
	ToolStarted   int64 `json:"tool_started"`
	ToolCompleted int64 `json:"tool_completed"`
	Messages      int64 `json:"messages"`
	Plans         int64 `json:"plans"`
}

type RunProgress struct {
	Observation ProgressObservation `json:"observation"`
	CheckedAt   time.Time           `json:"checked_at"`
	State       string              `json:"state"`
}

func ValidateProgress(v ProgressObservation) error {
	for _, n := range []int64{v.Sequence, v.ToolStarted, v.ToolCompleted, v.Messages, v.Plans} {
		if n < 0 || n > 1<<53-1 {
			return Fail("invalid_input", "bounded nonnegative progress counters required")
		}
	}
	if v.Sequence == 0 {
		return Fail("invalid_input", "progress sequence required")
	}
	return nil
}

// Only an explicitly marked development Run may omit its execution deadline.
// Leases and authorization remain mandatory and independently renewable.
func ProgressManaged(r Run) bool {
	return r.ProgressVersion == ProgressVersion && r.Kind == "implement" && r.GrantSeconds == 0 && r.Deadline.IsZero()
}

func RunTimeValid(r Run, now time.Time) bool {
	return ProgressManaged(r) || r.GrantSeconds > 0 && !r.Deadline.IsZero() && now.Before(r.Deadline)
}
