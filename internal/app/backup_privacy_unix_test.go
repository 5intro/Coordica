//go:build !windows

package app

import (
	"os"
	"testing"
)

func assertBackupPrivate(t *testing.T, path string, directory bool) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	want := os.FileMode(0600)
	if directory {
		want = 0700
	}
	if info.IsDir() != directory || info.Mode().Perm() != want {
		t.Fatalf("backup privacy for %s: mode %v, want %o (directory=%v)", path, info.Mode(), want, directory)
	}
}
