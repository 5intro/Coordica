package app

import (
	"time"

	"coordica/internal/domain"
	"coordica/internal/store"
)

// Both upload preflight and final acceptance use current transactional facts.
// A successful upload is material receipt, never permission to complete a Run.
func artifactUploadRun(tx *store.Tx, a Actor, id string, now time.Time) (domain.Run, error) {
	var run domain.Run
	if _, err := nodeConsoleActor(tx, a); err != nil {
		return run, err
	}
	if err := tx.Get("run", id, &run); err != nil {
		return run, err
	}
	if run.NodeID != a.NodeID {
		return run, domain.Fail("forbidden", "代码材料不属于本节点")
	}
	var task domain.Task
	if err := tx.Get("task", run.TaskID, &task); err != nil {
		return run, err
	}
	project, err := getProject(tx)
	if err != nil {
		return run, err
	}
	if run.Kind != "implement" || run.State != "running" || !now.Before(run.LeaseUntil) || !now.Before(run.Deadline) ||
		run.GrantSeconds <= 0 || run.ElapsedSeconds >= run.GrantSeconds || task.UsedSeconds >= task.BudgetSeconds ||
		task.CurrentRunID != run.ID || task.Generation != run.Generation || task.State != "executing" || task.HoldReason != "" ||
		project.Paused || project.PolicyVersion != run.PolicyVersion {
		return run, domain.Fail("stale_run", "当前执行已失效，不能接收代码材料")
	}
	if err := baselineRun(tx, run); err != nil {
		return run, err
	}
	if !grantedGoalAuthority(tx, &task, run) {
		return run, domain.Fail("stale_run", "当前目标授权已失效，不能接收代码材料")
	}
	return run, nil
}
