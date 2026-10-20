package filelock

import (
	"context"
	"errors"
	"time"
)

var ErrBusy = errors.New("file lock is held")

// AcquireContext waits only for lock contention. Filesystem/permission errors
// return immediately, and cancellation never leaves a newly acquired lock held.
func AcquireContext(ctx context.Context, path string) (func(), error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		release, err := Acquire(path)
		if err == nil {
			if err := ctx.Err(); err != nil {
				release()
				return nil, err
			}
			return release, nil
		}
		if !errors.Is(err, ErrBusy) {
			return nil, err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
