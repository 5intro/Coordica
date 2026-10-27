package app

import (
	"context"
	"coordica/internal/domain"
	"coordica/internal/store"
	"time"
)

func executePlanningLeader(execCtx context.Context, w *PlanningWorker, r domain.PlanningRun, dir, prompt string) (PlanningOutcome, error) {
	outcome, err := executeObservedLeader(execCtx, w.Service,
		domain.LeaderExecutionRef{Kind: "planning", ID: r.ID, Generation: r.Generation, InputVersion: r.DocumentVersion},
		w.Executor, pinnedLeader(r.Leader), dir, prompt, func(pid int) error {
			return w.Service.DB.Update(execCtx, func(tx *store.Tx) error {
				var current domain.PlanningRun
				if e := tx.Get("planning_run", r.ID, &current); e != nil {
					return e
				}
				if current.State != "running" || current.Generation != r.Generation || !failurePlanningCurrent(tx, current) || current.Purpose == domain.ConsultationPurpose && !consultationCurrent(tx, current) {
					return domain.Fail("stale_run", "planning authority changed before process-start receipt")
				}
				current.ProcessID = pid
				return tx.Put("planning_run", r.ID, current)
			})
		})
	return outcome, err
}

func watchPlanningLeader(execCtx context.Context, w *PlanningWorker, r domain.PlanningRun,
	watchDone <-chan struct{}, watchExited chan<- struct{}, cancel context.CancelFunc,
) {
	defer close(watchExited)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-watchDone:
			return
		case <-execCtx.Done():
			return
		case <-ticker.C:
			err := w.Service.DB.Update(execCtx, func(tx *store.Tx) error {
				var current domain.PlanningRun
				if e := tx.Get("planning_run", r.ID, &current); e != nil {
					return e
				}
				var doc domain.PlanningDocument
				if current.Purpose == domain.ConsultationPurpose {
					var e error
					doc, e = goalDocument(tx)
					if e != nil {
						return e
					}
					if !consultationCurrent(tx, current) {
						return context.Canceled
					}
				} else if e := tx.Get("planning_document", "current", &doc); e != nil {
					return e
				}
				p, e := getProject(tx)
				if e != nil {
					return e
				}
				baseline, initErr := initialization(tx)
				if initErr != nil {
					return initErr
				}
				if current.Purpose != domain.ConsultationPurpose && (baselineGate(tx, nil, false) != nil ||
					current.BaselineVersion != baseline.ApprovedVersion || current.BaselineDigest != baseline.ApprovedDigest) {
					return context.Canceled
				}
				if current.State != "running" || current.Generation != r.Generation || doc.Version != r.DocumentVersion ||
					p.Paused || !w.Service.Now().Before(current.LeaseUntil) ||
					current.Purpose != domain.ConsultationPurpose && !requirementPlanCurrent(tx, current) || !failurePlanningCurrent(tx, current) {
					return context.Canceled
				}
				current.LeaseUntil = w.Service.Now().Add(w.Service.Lease)
				if current.Deadline.Before(current.LeaseUntil) {
					current.LeaseUntil = current.Deadline
				}
				return tx.Put("planning_run", r.ID, current)
			})
			if err != nil {
				cancel()
				return
			}
		}
	}

}

// Keep renewal alive through durable receipt and completion, as before extraction.
func startPlanningWatch(ctx context.Context, w *PlanningWorker, r domain.PlanningRun) (context.Context, func()) {
	execCtx, cancel := context.WithDeadline(ctx, r.Deadline)
	watchDone := make(chan struct{})
	watchExited := make(chan struct{})
	finish := func() { close(watchDone); cancel(); <-watchExited }
	go watchPlanningLeader(execCtx, w, r, watchDone, watchExited, cancel)
	return execCtx, finish
}
