package runtime

import (
	"context"
	"math"

	"coordica/internal/store"
)

func (l *Ledger) CancelRun(ctx context.Context, credential Credential, request CancelRequest) (Run, error) {
	if !identifier(request.TaskID, 128) || !identifier(request.RunID, 128) || request.ExpectedVersion <= 0 || request.RunGeneration <= 0 {
		return Run{}, ErrInvalid
	}
	var task Task
	var run Run
	return mutate(l, ctx, credential, Cancel, request.Key, "run.cancelled", request,
		func(tx *store.Tx, grant Grant) (err error) {
			if task, err = taskFor(tx, grant, request.TaskID); err == nil {
				run, err = runFor(tx, grant, task.ID, request.RunID, false)
			}
			if err == nil && run.Generation != request.RunGeneration {
				err = ErrConflict
			}
			return err
		}, func(tx *store.Tx, _ Grant) (Run, string, bool, error) {
			if run.State == "cancelled" {
				return run, run.ID, false, nil
			}
			if err := writable(task, request.ExpectedVersion); err != nil {
				return Run{}, "", false, err
			}
			if task.CurrentRunID != run.ID || run.State != "registered" || run.Version <= 0 || run.Version == math.MaxInt64 {
				return Run{}, "", false, ErrConflict
			}
			run.State, run.Version = "cancelled", run.Version+1
			task.CurrentRunID, task.Version = "", task.Version+1
			if err := tx.Put(kindRun, run.ID, run); err != nil {
				return Run{}, "", false, err
			}
			return run, run.ID, true, tx.Put(kindTask, task.ID, task)
		})
}

func (l *Ledger) CancelTask(ctx context.Context, credential Credential, request CancelRequest) (Task, error) {
	if !identifier(request.TaskID, 128) || request.ExpectedVersion <= 0 || request.RunID != "" || request.RunGeneration != 0 {
		return Task{}, ErrInvalid
	}
	var task Task
	var run Run
	return mutate(l, ctx, credential, Cancel, request.Key, "task.cancelled", request,
		func(tx *store.Tx, grant Grant) (err error) {
			if task, err = taskFor(tx, grant, request.TaskID); err == nil && task.CurrentRunID != "" {
				run, err = runFor(tx, grant, task.ID, task.CurrentRunID, false)
			}
			return err
		}, func(tx *store.Tx, _ Grant) (Task, string, bool, error) {
			if task.State == "cancelled" {
				return task, task.ID, false, nil
			}
			if err := writable(task, request.ExpectedVersion); err != nil {
				return Task{}, "", false, err
			}
			if task.CurrentRunID != "" {
				if run.State != "registered" || run.Version <= 0 || run.Version == math.MaxInt64 {
					return Task{}, "", false, ErrConflict
				}
				run.State, run.Version = "cancelled", run.Version+1
				if err := tx.Put(kindRun, run.ID, run); err != nil {
					return Task{}, "", false, err
				}
			}
			task.State, task.Version = "cancelled", task.Version+1
			return task, task.ID, true, tx.Put(kindTask, task.ID, task)
		})
}
