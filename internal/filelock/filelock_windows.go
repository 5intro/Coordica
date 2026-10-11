//go:build windows

package filelock

import (
	"sync"
	"syscall"
)

// Acquire takes an exclusive, non-inheritable file handle. A competing owner
// receives a sharing violation immediately. Keep the file instead of deleting
// it on release. The parent directory must already exist.
func Acquire(path string) (func(), error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	var once sync.Once
	return func() { once.Do(func() { syscall.CloseHandle(h) }) }, nil
}
