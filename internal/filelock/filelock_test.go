package filelock

import (
	"path/filepath"
	"testing"
)

func TestExclusiveOwnershipAndReacquire(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owner.lock")
	release, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if other, err := Acquire(path); err == nil {
		other()
		t.Fatal("second owner acquired the same directory")
	}
	release()
	release() // Release is safe if shutdown paths converge.
	other, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	other()
}
