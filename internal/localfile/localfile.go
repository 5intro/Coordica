// Package localfile stores private local state without coupling file security
// to task state, agents, credentials, or databases.
package localfile

import (
	"os"
	"path/filepath"
)

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
