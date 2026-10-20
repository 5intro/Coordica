package filelock

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestWaitHonorsContentionCancellationAndPermissionErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.lock")
	release, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if other, err := Acquire(path); !errors.Is(err, ErrBusy) {
		if other != nil {
			other()
		}
		t.Fatal("contention not classified", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if other, err := AcquireContext(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		if other != nil {
			other()
		}
		t.Fatal("lock wait not cancellable", err)
	}
	release()
	if other, err := AcquireContext(ctx, path); err == nil {
		other()
		t.Fatal("expired caller acquired lock")
	}
	if other, err := AcquireContext(context.Background(), filepath.Join(t.TempDir(), "missing", "lock")); err == nil || errors.Is(err, ErrBusy) {
		if other != nil {
			other()
		}
		t.Fatal("filesystem failure retried", err)
	}
	other, err := AcquireContext(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	other()
}
