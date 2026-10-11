package runner

import (
	"errors"
	"runtime"

	"coordica/internal/domain"
	"coordica/internal/process"
)

func recordedTreeQuiescent(pid int) (bool, error) {
	if runtime.GOOS == "windows" {
		// A missing parent does not observe every process in an uncertain Job,
		// including after restart. Only an already confirmed handoff or center
		// stop confirmation can release this otherwise unknown execution.
		return false, errors.New("Windows uncertain process tree requires independent stop confirmation")
	}
	return process.TreeQuiescent(pid)
}

// recheckStopped advances only an acknowledged, uncertain handoff. An
// unacknowledged completion must retain its exact request and idempotency key.
func (r *Runner) recheckStopped(path string, record spool) (spool, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active[record.Assignment.Run.ID] || record.Request == nil || !record.Acknowledged || record.Request.Quiescent {
		return record, false, nil
	}
	quiet, err := recordedTreeQuiescent(record.PID)
	if err != nil || !quiet {
		return record, false, err
	}
	record.Request = &domain.CompleteRequest{
		Generation:     record.Assignment.Run.Generation,
		ElapsedSeconds: record.Request.ElapsedSeconds,
		Status:         "stopped",
		SessionID:      record.Request.SessionID,
		Detail:         "Runner confirmed the recorded process group stopped; preserved workspace",
		Quiescent:      true,
	}
	record.Key = randomKey()
	record.Acknowledged = false
	if err := writePrivateJSON(path, record); err != nil {
		return record, false, err
	}
	return record, true, nil
}
