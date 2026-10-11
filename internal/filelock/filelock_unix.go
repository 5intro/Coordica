//go:build !windows

// Package filelock provides nonblocking process ownership of a local data
// directory. Keep the returned release function for the owner's whole lifetime.
package filelock

import (
	"os"
	"sync"
	"syscall"
)

// Acquire opens a stable lock file and takes an exclusive lock. The file must
// not be deleted while an owner may exist; deletion permits another inode to
// be locked. The parent directory must already exist.
func Acquire(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	var once sync.Once
	return func() { once.Do(func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }) }, nil
}
