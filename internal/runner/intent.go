package runner

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/process"
)

//go:embed execution.schema.json
var executionOutputSchema []byte

const maxIntentAttempts = 3

// A pending receipt retains the exact request and actual stop observation.
// Its completion is not submitted until the receipt is settled. Neither a
// receipt nor this record is a grant; only the application's assessment grants.
type intentHandoff struct {
	Intent     domain.ExecutionIntent `json:"outcome"`
	Key        string                 `json:"key"`
	Attempts   int                    `json:"attempts"`
	Settled    bool                   `json:"settled"`
	Completion domain.CompleteRequest `json:"completion"`
}

func intentKey(run domain.Run) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("coordica.execution.v1:%s:%d", run.ID, run.Generation)))
	return fmt.Sprintf("execution-v1-%x", sum[:])
}

// attemptIntent performs at most one network attempt. The finite attempt count
// and exact body/key survive a crash, including a committed response lost in
// transit. A stale/rejected or exhausted receipt produces only a stopped handoff.
func (r *Runner) attemptIntent(ctx context.Context, record *spool) error {
	p := record.Intent
	if p == nil || p.Settled {
		return nil
	}
	encoded, err := json.Marshal(p.Intent)
	intent, parseErr := domain.ParseExecutionIntent(string(encoded))
	if err != nil || parseErr != nil || intent.Status == "completed" || record.Assignment.GoalContext == nil || record.Assignment.Run.Kind != "implement" || p.Key != intentKey(record.Assignment.Run) || p.Attempts < 0 || p.Attempts > maxIntentAttempts || !p.Completion.Quiescent || p.Completion.Generation != record.Assignment.Run.Generation || p.Completion.Status != "stopped" || p.Completion.IntentStatus != intent.Status {
		return errors.New("pending native intent lacks exact stopped Run evidence")
	}
	if intent.Status == "scope_requested" {
		if _, err := gitops.NewScope(intent.RequestedPaths, nil); err != nil {
			return err
		}
		base, err := gitops.ParseOID(record.Assignment.Run.Base)
		if err == nil {
			err = r.git.VerifyClean(ctx, record.WorkDir, base)
		}
		if err != nil {
			// Recheck physical inputs before every replay, not merely before
			// persisting the original receipt. Never snapshot to make it clean.
			p.Settled = true
			p.Completion.Detail = r.safeText("native scope receipt suppressed; fixed workspace cannot be verified unchanged: " + shortDetail(err))
			if errors.Is(err, process.ErrTreeNotStopped) {
				p.Completion.Quiescent = false
				record.PID = 0 // Agent PID cannot prove a later Git tree stopped.
			}
			if saveErr := writePrivateJSON(r.spoolPath(record.Assignment.Run.ID), record); saveErr != nil {
				return errors.Join(err, saveErr)
			}
			if errors.Is(err, process.ErrTreeNotStopped) {
				return err
			}
			return nil
		}
	}
	if ctx.Err() != nil {
		p.Settled = true
		p.Completion.Detail = "native intent submission cancelled; no permission granted"
		return writePrivateJSON(r.spoolPath(record.Assignment.Run.ID), record)
	}
	if p.Attempts >= maxIntentAttempts {
		p.Settled = true
		p.Completion.Detail = "native intent receipt unavailable after bounded delivery; no scope granted"
		return writePrivateJSON(r.spoolPath(record.Assignment.Run.ID), record)
	}
	p.Attempts++
	if err := writePrivateJSON(r.spoolPath(record.Assignment.Run.ID), record); err != nil {
		return err
	}
	run := record.Assignment.Run
	err = nil
	switch p.Intent.Status {
	case "scope_requested":
		var receipt domain.ScopeAssessment
		_, err = r.json(ctx, "POST", "/api/v1/runs/"+run.ID+"/request_scope", p.Key, domain.RunScopeRequest{Generation: run.Generation, Paths: p.Intent.RequestedPaths, Reason: p.Intent.Reason}, &receipt)
		if err == nil && (receipt.ID == "" || receipt.Version != 1 || receipt.RunID != run.ID || receipt.Generation != run.Generation || receipt.TaskID != record.Assignment.Task.ID || receipt.State != "waiting_stop" || receipt.Reason != p.Intent.Reason || !slices.Equal(receipt.Paths, p.Intent.RequestedPaths)) {
			err = errors.New("scope receipt does not match the original Run and task")
		}
	case "blocked":
		var receipt domain.Task
		input := struct {
			Generation int64  `json:"generation"`
			Reason     string `json:"reason"`
		}{run.Generation, p.Intent.Reason}
		_, err = r.json(ctx, "POST", "/api/v1/runs/"+run.ID+"/block", p.Key, input, &receipt)
		if err == nil && receipt.ID != record.Assignment.Task.ID {
			err = errors.New("block receipt does not match the original task")
		}
	default:
		return errors.New("invalid persisted native intent")
	}
	var remote *apiError
	definite := errors.As(err, &remote) && remote.Status >= 400 && remote.Status < 500 && remote.Status != http.StatusRequestTimeout && remote.Status != http.StatusTooManyRequests
	if err == nil || definite || ctx.Err() != nil || p.Attempts >= maxIntentAttempts {
		p.Settled = true
		if err != nil {
			p.Completion.Detail = r.safeText("native intent receipt rejected or unavailable; no scope granted: " + shortDetail(err))
		}
		if saveErr := writePrivateJSON(r.spoolPath(run.ID), record); saveErr != nil {
			return saveErr
		}
		return nil
	}
	return err
}
