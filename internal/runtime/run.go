package runtime

import (
	"context"
	"math"
	"time"

	"coordica/internal/store"
)

func validTarget(target Target) bool {
	return identifier(target.InstanceID, 128) && target.InstanceVersion > 0 && identifier(target.NodeID, 128) &&
		identifier(target.Harness, 128) && (target.Model == "" || identifier(target.Model, 128)) &&
		(target.Effort == "" || identifier(target.Effort, 128)) && identifier(target.WorkspaceRef, 512)
}

func runFor(tx *store.Tx, grant Grant, taskID, id string, prepare bool) (Run, error) {
	var run Run
	err := tx.Get(kindRun, id, &run)
	if err == nil && (run.ApplicationID != grant.ApplicationID || run.TaskID != taskID) {
		err = ErrForbidden
	}
	if err == nil {
		_, err = targetFor(tx, grant, run.Target, prepare)
	}
	return run, err
}

func runSnapshot(tx *store.Tx, run Run) (RunSnapshot, error) {
	var material TaskContext
	err := tx.Get(kindContext, run.ContextID, &material)
	if err == nil && (material.TaskID != run.TaskID || material.ApplicationID != run.ApplicationID || material.Version != run.ContextVersion) {
		err = ErrConflict
	}
	return RunSnapshot{Run: run, Context: material}, err
}

// RegisterRun persists an explicit target and immutable context/authority
// snapshot. Registered/not_started is not a native start, lease or stop receipt.
func (l *Ledger) RegisterRun(ctx context.Context, credential Credential, request RegisterRunRequest) (RunSnapshot, error) {
	if !identifier(request.TaskID, 128) || request.ExpectedVersion <= 0 || request.ExpectedContextVersion <= 0 || !validTarget(request.Target) {
		return RunSnapshot{}, ErrInvalid
	}
	id, err := freshID("run")
	if err != nil {
		return RunSnapshot{}, err
	}
	var task Task
	var instance Instance
	return mutate(l, ctx, credential, Register, request.Key, "run.registered", request,
		func(tx *store.Tx, grant Grant) (err error) {
			if task, err = taskFor(tx, grant, request.TaskID); err == nil {
				instance, err = targetFor(tx, grant, request.Target, true)
			}
			return err
		}, func(tx *store.Tx, grant Grant) (RunSnapshot, string, bool, error) {
			if err := writable(task, request.ExpectedVersion); err != nil {
				return RunSnapshot{}, "", false, err
			}
			if task.ContextVersion != request.ExpectedContextVersion || task.CurrentRunID != "" ||
				task.RunGeneration < 0 || task.RunGeneration == math.MaxInt64 {
				return RunSnapshot{}, "", false, ErrConflict
			}
			task.Version++
			task.RunGeneration++
			run := Run{ID: id, ApplicationID: grant.ApplicationID, TaskID: task.ID, Version: 1,
				Attempt: task.RunGeneration, Generation: task.RunGeneration, Target: request.Target,
				InstanceConfigRef: instance.ConfigRef, ContextID: task.ContextID, ContextVersion: task.ContextVersion,
				AuthorizationVersion: grant.Version, PrincipalID: grant.ID, State: "registered",
				NativeDisposition: "not_started", CreatedAt: time.Now().UTC()}
			task.CurrentRunID = run.ID
			if err := putNew(tx, kindRun, run.ID, run); err != nil {
				return RunSnapshot{}, "", false, err
			}
			if err := tx.Put(kindTask, task.ID, task); err != nil {
				return RunSnapshot{}, "", false, err
			}
			result, err := runSnapshot(tx, run)
			return result, run.ID, true, err
		})
}

func (l *Ledger) ReadRun(ctx context.Context, credential Credential, taskID, id string) (RunSnapshot, error) {
	var result RunSnapshot
	if l == nil || l.db == nil || !identifier(taskID, 128) || !identifier(id, 128) {
		return result, ErrInvalid
	}
	err := l.db.View(ctx, func(tx *store.Tx) error {
		grant, err := authorize(tx, credential, Read)
		if err != nil {
			return err
		}
		if _, err = taskFor(tx, grant, taskID); err != nil {
			return err
		}
		run, err := runFor(tx, grant, taskID, id, false)
		if err == nil {
			result, err = runSnapshot(tx, run)
		}
		return err
	})
	if err != nil {
		return RunSnapshot{}, err
	}
	return result, nil
}

func (l *Ledger) RecordContinuation(ctx context.Context, credential Credential, request ContinuationRequest) (Input, error) {
	if !identifier(request.TaskID, 128) || !identifier(request.RunID, 128) || request.ExpectedVersion <= 0 ||
		request.RunGeneration <= 0 || request.ContextVersion <= 0 || !sourceValid(request.Source) || !content(request.Content) {
		return Input{}, ErrInvalid
	}
	var task Task
	var run Run
	return mutate(l, ctx, credential, Continue, request.Key, "continuation.recorded", request,
		func(tx *store.Tx, grant Grant) (err error) {
			if task, err = taskFor(tx, grant, request.TaskID); err == nil {
				run, err = runFor(tx, grant, task.ID, request.RunID, true)
			}
			if err == nil && (run.Generation != request.RunGeneration || run.ContextVersion != request.ContextVersion) {
				err = ErrConflict
			}
			return err
		}, func(tx *store.Tx, grant Grant) (Input, string, bool, error) {
			body := inputBody{Kind: "continuation", TaskID: task.ID, Content: request.Content,
				RunID: run.ID, RunGeneration: run.Generation, ContextVersion: run.ContextVersion}
			input, found, err := priorInput(tx, grant, request.Source, body)
			if err != nil || found {
				return input, input.ID, false, err
			}
			if err = writable(task, request.ExpectedVersion); err != nil {
				return Input{}, "", false, err
			}
			if task.CurrentRunID != run.ID || run.State != "registered" {
				return Input{}, "", false, ErrConflict
			}
			task.Version++
			input.ContextID, input.ContextVersion = run.ContextID, run.ContextVersion
			input.TaskVersion, input.CreatedAt = task.Version, time.Now().UTC()
			if err = putNew(tx, kindInput, input.ID, input); err == nil {
				err = tx.Put(kindTask, task.ID, task)
			}
			return input, input.ID, true, err
		})
}
