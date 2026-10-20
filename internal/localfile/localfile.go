// Package localfile stores private local state without coupling file security
// to task state, agents, credentials, or databases.
package localfile

import (
	"os"
	"path/filepath"
)

// MkdirAllDurable creates a directory tree and persists its directory entries
// before callers perform an external action that must not be repeated after a
// crash. On Unix, sync every ancestor, including existing ones: another creator
// or an earlier failed sync can leave an entry visible but not yet durable.
// Windows has no POSIX directory fsync; WritePrivate uses a write-through move.
func MkdirAllDurable(path string, mode os.FileMode) error {
	return mkdirAllDurable(path, mode, syncDirectory)
}

func mkdirAllDurable(path string, mode os.FileMode, syncDir func(string) error) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(path, mode); err != nil {
		return err
	}
	for {
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		if err := syncDir(parent); err != nil {
			return err
		}
		path = parent
	}
}

// WritePrivate writes a private temporary file before replacing path atomically.
// Its parent directory is created as needed. To protect companion files made
// by other programs, callers should also Protect their data directory.
func WritePrivate(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	// Set NTFS permissions before sensitive content is ever written.
	if err = Protect(f.Name()); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return Replace(f.Name(), path)
}
