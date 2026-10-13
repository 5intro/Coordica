//go:build !windows

package localfile

import (
	"os"
	"path/filepath"
)

// Protect limits a file to its owner and a directory to owner traversal.
func Protect(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	mode := os.FileMode(0600)
	if info.IsDir() {
		mode = 0700
	}
	return os.Chmod(path, mode)
}

// Replace atomically installs source and fsyncs the destination directory.
func Replace(source, dest string) error {
	if err := os.Rename(source, dest); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(dest))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// CheckPrivate verifies without changing existing file permissions.
func CheckPrivate(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return os.ErrPermission
	}
	return nil
}
