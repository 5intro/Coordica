package runner

import (
	"context"
	"errors"
	"sync"
	"time"

	"coordica/internal/domain"
)

type permissionWindow struct {
	mu                           sync.Mutex
	lease, deadline, lastSuccess time.Time
}

func (w *permissionWindow) until() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	until := w.lease
	for _, limit := range []time.Time{w.deadline, w.lastSuccess.Add(30 * time.Second)} {
		if limit.Before(until) {
			until = limit
		}
	}
	return until
}

func (w *permissionWindow) renew(lease, success time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lease, w.lastSuccess = lease, success
}

func (r *Runner) watchPermission(ctx context.Context, stop context.CancelFunc, assignment domain.Assignment, started, lease, deadline, lastSuccess time.Time,
) (func(), func() time.Time) {
	heartCtx, end := context.WithCancel(ctx)
	window := &permissionWindow{lease: lease, deadline: deadline, lastSuccess: lastSuccess}
	interval := 10 * time.Second
	if assignment.HeartbeatSeconds > 0 && assignment.HeartbeatSeconds < 10 {
		interval = time.Duration(assignment.HeartbeatSeconds) * time.Second
	}
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-heartCtx.Done():
				return
			case now := <-ticker.C:
				if !now.Before(window.until()) {
					r.report("permission.expired", assignment.Run.ID, "lease, deadline, or 30-second heartbeat silence exceeded")
					stop()
					return
				}
			}
		}
	}()
	go func() {
		defer workers.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-heartCtx.Done():
				return
			case <-ticker.C:
				callCtx, cancel := context.WithTimeout(heartCtx, 5*time.Second)
				var response domain.HeartbeatResponse
				clock, err := r.json(callCtx, "POST", "/api/v1/runs/"+assignment.Run.ID+"/heartbeat", "", domain.HeartbeatRequest{
					Generation: assignment.Run.Generation, ElapsedSeconds: int64(time.Since(started) / time.Second), Detail: "managed activity",
				}, &response)
				cancel()
				if err != nil {
					r.report("heartbeat.failed", assignment.Run.ID, shortDetail(err))
					var remote *apiError
					if errors.As(err, &remote) && (remote.Status == 401 || remote.Status == 403 || remote.Status == 409) {
						stop()
						return
					}
					continue
				}
				if response.Cancel {
					r.report("permission.revoked", assignment.Run.ID, response.Reason)
					stop()
					return
				}
				newLease, err := localDeadline(response.LeaseUntil, clock)
				if err != nil {
					r.report("heartbeat.invalid", assignment.Run.ID, shortDetail(err))
					stop()
					return
				}
				window.renew(newLease, clock.Received)
			}
		}
	}()
	return func() { end(); workers.Wait() }, window.until
}
