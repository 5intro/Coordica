//go:build windows

package filelock

import (
	"errors"
	"fmt"
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
		if errors.Is(err, syscall.Errno(32)) {
			return nil, fmt.Errorf("%w: %w", ErrBusy, err)
		} // ERROR_SHARING_VIOLATION
		return nil, err
	}
	var once sync.Once
	return func() { once.Do(func() { syscall.CloseHandle(h) }) }, nil
}
