package localfile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

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
