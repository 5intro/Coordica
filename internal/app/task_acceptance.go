package app

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"coordica/internal/domain"
	"coordica/internal/store"
)

// This immutable receipt binds old material to a new Owner-authored contract.
// It confers no verdict, test result, scope expansion or running permission.
type taskAcceptanceRevision struct {
	Before               domain.Task `json:"before"`
	AuthorizationVersion int64       `json:"authorization_version"`
	Acceptance           string      `json:"acceptance"`
	CandidateID          string      `json:"candidate_id"`
	Commit               string      `json:"commit"`
	Main                 string      `json:"main"`
	Head                 string      `json:"head"`
	SourceRunID          string      `json:"source_run_id"`
	PolicyVersion        int64       `json:"policy_version"`
	Reason               string      `json:"reason"`
	ActorID              string      `json:"actor_id"`
	CreatedAt            time.Time   `json:"created_at"`
}

func ReviseTaskAcceptance(ctx context.Context, s *Service, a Actor, key, id string, in domain.TaskAcceptanceRequest) (json.RawMessage, error) {
	if err := owner(a); err != nil {
		return nil, err
	}
	if err := domain.ValidateTaskAcceptanceRequest(in); err != nil {
		return nil, err
	}
	return s.mutate(ctx, a, key, "task.acceptance."+id, in, func(tx *store.Tx) (any, error) {
		var task domain.Task
		if err := tx.Get("task", id, &task); err != nil {
			return nil, err
		}
		if err := version(task.Version, in.ExpectedVersion); err != nil {
			return nil, err
		}
		if err := requireQuiescence(tx, id); err != nil {
			return nil, err
		}
		if task.CurrentRunID != "" || task.ReservedSeconds != 0 || task.Verification != nil || task.ApprovedVersion == 0 ||
			task.CurrentCandidateID != in.ExpectedCandidateID || task.Repairs >= task.MaxRepairs ||
			(task.HoldReason != "human_decision" && task.HoldReason != "paused") || domain.TaskFinished(task) {
			return nil, domain.Fail("conflict", "a stopped approved task with a rejected candidate and remaining repair allowance is required")
		}
		if task.Acceptance == in.Acceptance {
			return nil, domain.Fail("invalid_input", "the acceptance contract must actually change")
		}
		if err := baselineGate(tx, nil, false); err != nil {
			return nil, err
		}
		if err := goalGate(tx, &task, false); err != nil {
			return nil, err
		}
		project, err := getProject(tx)
		if err != nil {
			return nil, err
		}
		var candidate domain.Candidate
		if err = tx.Get("candidate", in.ExpectedCandidateID, &candidate); err != nil {
			return nil, err
		}
		if err = rejectedRepairSource(tx, task, project, candidate); err != nil {
			return nil, err
		}
		if err = queueTaskPullReviewRevocations(tx, id); err != nil {
			return nil, err
		}
		revision := taskAcceptanceRevision{Before: task, AuthorizationVersion: task.AuthorizationVersion + 1,
			Acceptance: in.Acceptance, CandidateID: candidate.ID, Commit: candidate.Commit, Main: candidate.Main,
			Head: candidate.Head, SourceRunID: candidate.SourceRunID, PolicyVersion: candidate.PolicyVersion,
			Reason: in.Reason, ActorID: a.ID, CreatedAt: s.Now()}
		task.Acceptance, task.AuthorizationVersion = in.Acceptance, revision.AuthorizationVersion
		task.Version++
		task.Repairs++
		task.CurrentCandidateID, task.State, task.HoldReason = "", "ready", "paused"
		if err = tx.Put("task_acceptance_revision", id+"_"+strconv.FormatInt(task.AuthorizationVersion, 10), revision); err != nil {
			return nil, err
		}
		if err = tx.Put("task_acceptance_input", id, revision); err != nil {
			return nil, err
		}
		if err = tx.Put("task", id, task); err != nil {
			return nil, err
		}
		return task, s.audit(tx, a, "task.acceptance_revised", id, candidate.ID+": "+in.Reason)
	})
}
