package app

import (
	"context"
	"coordica/internal/domain"
	"coordica/internal/store"
	"encoding/json"
	"strings"
)

// An Owner can retry a failed program preparation using the existing immutable
// handoff. This is neither a code repair nor acceptance of a previous review.
// The ordinary task version/idempotency/audit transaction is owned by TaskAction.
func (s *Service) retryCandidatePreparation(tx *store.Tx, a Actor, t *domain.Task) error {
	if t.State != "awaiting_integration" || t.CurrentCandidateID == "" || t.CurrentRunID != "" || t.ApprovedVersion == 0 || (t.HoldReason != "human_decision" && t.HoldReason != "paused" && t.HoldReason != "") {
		return domain.Fail("conflict", "only a stopped preparation failure can be retried")
	}
	if err := requireQuiescence(tx, t.ID); err != nil {
		return err
	}
	if err := goalGate(tx, t, false); err != nil {
		return err
	}
	if t.Retries >= t.MaxRetries || t.UsedSeconds+t.ReservedSeconds >= t.BudgetSeconds {
		return domain.Fail("budget", "task retries or budget exhausted")
	}
	var c domain.Candidate
	if err := tx.Get("candidate", t.CurrentCandidateID, &c); err != nil {
		return err
	}
	p, err := getProject(tx)
	if err != nil {
		return err
	}
	if c.State != "rejected" || c.Commit != "" || c.TaskID != t.ID || c.AuthorizationVersion != t.AuthorizationVersion || c.PolicyVersion != p.PolicyVersion || c.BaselineVersion != t.BaselineVersion || c.BaselineDigest != t.BaselineDigest {
		return domain.Fail("conflict", "fixed reviewed candidates and stale authority cannot be retried as preparation")
	}
	if !c.PreparationStopped {
		return domain.Fail("quiescence", "preparation process stop requires Owner evidence")
	}
	var source domain.Run
	if err = tx.Get("run", c.SourceRunID, &source); err != nil {
		return err
	}
	var artifact domain.Artifact
	if err = tx.Get("candidate_artifact", c.ID, &artifact); err != nil {
		return err
	}
	if source.TaskID != t.ID || source.Kind != "implement" || source.State != "succeeded" || !source.Quiescent || source.Head != c.Head || source.Base != c.Base || artifact.RunID != source.ID || source.AuthorizationVersion != t.AuthorizationVersion || source.PolicyVersion != p.PolicyVersion || source.GoalVersion != t.GoalVersion || source.GoalAuthorizationVersion != t.GoalAuthorizationVersion {
		return domain.Fail("conflict", "original stopped handoff required")
	}
	c.State = "accepted"
	c.PreparationStopped = false
	c.Main = ""
	c.Review = nil
	c.Checks = nil
	c.CheckPlan = nil
	c.CheckRunID = ""
	c.ApprovedCommit = ""
	c.Detail = "Owner requested bounded preparation retry"
	c.Version++
	t.HoldReason = ""
	t.Retries++
	if err = tx.Put("candidate", c.ID, c); err != nil {
		return err
	}
	decisions, err := store.List[domain.Decision](tx, "decision")
	if err != nil {
		return err
	}
	for _, d := range decisions {
		if d.Status == "open" && d.SourceKind == "task" && d.SourceID == t.ID && d.SourceVersion == t.Version {
			d.Status = "superseded"
			d.Resolution = "Owner requested bounded preparation retry"
			d.Version++
			if err = tx.Put("decision", d.ID, d); err != nil {
				return err
			}
		}
	}
	return s.audit(tx, a, "candidate.preparation_retry", c.ID, c.Head)
}

// Legacy failures and unknown Git trees have no positive program stop receipt.
// Like Run stop confirmation, this records an Owner's explicit observation;
// it never changes code, task authority, retry counters or a review verdict.
func (s *Service) ConfirmPreparationStopped(ctx context.Context, a Actor, key, id string, req domain.StopConfirmation) (json.RawMessage, error) {
	if err := owner(a); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Evidence) == "" || len(req.Evidence) > 16384 {
		return nil, domain.Fail("invalid_input", "bounded process-stop evidence required")
	}
	return s.mutate(ctx, a, key, "task."+id+".preparation-stopped", req, func(tx *store.Tx) (any, error) {
		var task domain.Task
		if err := tx.Get("task", id, &task); err != nil {
			return nil, err
		}
		if err := version(task.Version, req.ExpectedVersion); err != nil {
			return nil, err
		}
		if task.State != "awaiting_integration" || task.CurrentCandidateID == "" {
			return nil, domain.Fail("conflict", "current failed preparation required")
		}
		if err := requireQuiescence(tx, id); err != nil {
			return nil, err
		}
		var c domain.Candidate
		if err := tx.Get("candidate", task.CurrentCandidateID, &c); err != nil {
			return nil, err
		}
		if c.TaskID != id || c.State != "rejected" || c.Commit != "" {
			return nil, domain.Fail("conflict", "fixed candidates are not preparation stop records")
		}
		c.PreparationStopped = true
		c.Version++
		task.Version++
		if err := tx.Put("candidate", c.ID, c); err != nil {
			return nil, err
		}
		if err := tx.Put("task", id, task); err != nil {
			return nil, err
		}
		if err := s.taskDecision(tx, task, ""); err != nil {
			return nil, err
		}
		return task, s.audit(tx, a, "candidate.preparation_stopped", c.ID, req.Evidence)
	})
}
