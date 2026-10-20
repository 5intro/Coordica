package localfile

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDurableDirectoryPersistsEveryAncestorAndRetriesFailedSync(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "catalog", "identity", "workspace")
	want := []string{}
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		want = append(want, dir)
		if filepath.Dir(dir) == dir {
			break
		}
	}
	syncFailure := errors.New("directory sync failed")
	calls := 0
	err := mkdirAllDurable(path, 0700, func(dir string) error {
		if calls >= len(want) || dir != want[calls] {
			t.Fatal("directory entries not synced in ancestor order", dir, calls)
		}
		calls++
		if calls == 2 {
			return syncFailure
		}
		return nil
	})
	if !errors.Is(err, syncFailure) || calls != 2 {
		t.Fatal("failed sync was ignored", err, calls)
	}
	// The directories now exist, but their previous failed sync cannot be
	// considered durable. Retrying must still sync the entire ancestor chain.
	calls = 0
	if err := mkdirAllDurable(path, 0700, func(dir string) error {
		if calls >= len(want) || dir != want[calls] {
			t.Fatal("retry skipped an existing ancestor", dir, calls)
		}
		calls++
		return nil
	}); err != nil || calls != len(want) {
		t.Fatal("incomplete directory sync", err, calls, want)
	}
	if err := MkdirAllDurable(path, 0700); err != nil {
		t.Fatal("native durable directory creation", err)
	}
}

func TestPrivateReplacementPreservesNewContentAndRemovesTemps(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "秘密 with spaces")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := Protect(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "身份.json")
	for _, value := range []string{"old", "new"} {
		if err := WritePrivate(path, []byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "new" {
		t.Fatal("replacement failed", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatal("temporary file retained", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("file permission", err)
		}
		info, err = os.Stat(dir)
		if err != nil || info.Mode().Perm() != 0700 {
			t.Fatal("directory permission", err)
		}
	}
}
