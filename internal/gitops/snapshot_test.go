package gitops

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func scopedFixture(t *testing.T) (*Client, string, OID, *Scope) {
	t.Helper()
	g := gitClient(t)
	repo := filepath.Join(t.TempDir(), "managed")
	if err := g.Init(context.Background(), repo, false); err != nil {
		t.Fatal(err)
	}
	writeFile(t, repo, "src/modify.txt", "base\n")
	writeFile(t, repo, "src/delete.txt", "delete\n")
	writeFile(t, repo, ".gitignore", "cache/\n")
	base, err := g.CommitAll(context.Background(), repo, "base", testIdentity)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Checkout(context.Background(), repo, base); err != nil {
		t.Fatal(err)
	}
	scope, err := NewScope([]string{"src/**"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return g, repo, base, scope
}

func TestCommitScopedSnapshotsCompleteAuthorizedInputs(t *testing.T) {
	g, repo, base, scope := scopedFixture(t)
	writeFile(t, repo, "src/modify.txt", "modified\n")
	writeFile(t, repo, "src/new.txt", "new\n")
	if err := os.Remove(filepath.Join(repo, "src/delete.txt")); err != nil {
		t.Fatal(err)
	}
	head, err := g.CommitScoped(context.Background(), repo, base, scope, "authorized snapshot", testIdentity)
	if err != nil {
		t.Fatal(err)
	}
	if head == base {
		t.Fatal("snapshot did not create a commit")
	}
	diff, err := g.CheckScope(context.Background(), repo, base, head, scope)
	if err != nil || len(diff.Changes) != 3 {
		t.Fatalf("incomplete snapshot: %+v %v", diff, err)
	}
	if err := g.VerifyClean(context.Background(), repo, head); err != nil {
		t.Fatal(err)
	}
	if parent := string(bytes.TrimSpace(mustRun(t, g, repo, "rev-parse", string(head)+"^"))); parent != base.String() {
		t.Fatal("snapshot parent is not fixed base")
	}
}

func TestCommitScopedRefusesUnsafeInputsWithoutTouchingIndexOrHead(t *testing.T) {
	cases := []struct {
		name   string
		change func(*testing.T, *Client, string)
		kind   error
	}{
		{"outside", func(t *testing.T, g *Client, repo string) { writeFile(t, repo, "outside.txt", "private outside\n") }, ErrOutOfScope},
		{"ignored", func(t *testing.T, g *Client, repo string) { writeFile(t, repo, "cache/private.txt", "ignored\n") }, ErrDirty},
		{"symlink", func(t *testing.T, g *Client, repo string) {
			if err := os.Symlink("modify.txt", filepath.Join(repo, "src/link")); err != nil {
				if runtime.GOOS == "windows" {
					t.Skip("Windows symlink privilege unavailable")
				}
				t.Fatal(err)
			}
		}, ErrUnsupportedObject},
		{"hidden index flag", func(t *testing.T, g *Client, repo string) {
			mustRun(t, g, repo, "update-index", "--assume-unchanged", "src/modify.txt")
		}, ErrDirty},
		{"staged input", func(t *testing.T, g *Client, repo string) {
			writeFile(t, repo, "src/staged.txt", "staged\n")
			mustRun(t, g, repo, "add", "--", "src/staged.txt")
		}, ErrDirty},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			g, repo, base, scope := scopedFixture(t)
			writeFile(t, repo, "src/modify.txt", "private intended\n")
			test.change(t, g, repo)
			before, err := os.ReadFile(filepath.Join(repo, ".git/index"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := g.CommitScoped(context.Background(), repo, base, scope, "must refuse", testIdentity); !errors.Is(err, test.kind) {
				t.Fatalf("error=%v want %v", err, test.kind)
			}
			head, err := g.Head(context.Background(), repo)
			if err != nil || head != base {
				t.Fatalf("HEAD changed: %s %v", head, err)
			}
			after, err := os.ReadFile(filepath.Join(repo, ".git/index"))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("original managed index changed on refusal")
			}
			content, err := os.ReadFile(filepath.Join(repo, "src/modify.txt"))
			if err != nil || string(content) != "private intended\n" {
				t.Fatal("private changes were discarded")
			}
		})
	}
}

func TestCommitScopedRejectsChangedHeadCancellationAndNoChanges(t *testing.T) {
	g, repo, base, scope := scopedFixture(t)
	if _, err := g.CommitScoped(context.Background(), repo, base, scope, "empty", testIdentity); !errors.Is(err, ErrNoChanges) {
		t.Fatal("empty snapshot accepted", err)
	}
	writeFile(t, repo, "src/modify.txt", "private edit\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := g.CommitScoped(ctx, repo, base, scope, "cancelled", testIdentity); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled snapshot accepted", err)
	}
	head, err := g.Head(context.Background(), repo)
	if err != nil || head != base {
		t.Fatal("cancelled snapshot advanced HEAD")
	}
	moved, err := g.CommitAll(context.Background(), repo, "other actor", testIdentity)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, repo, "src/new.txt", "keep private\n")
	if _, err := g.CommitScoped(context.Background(), repo, base, scope, "stale", testIdentity); !errors.Is(err, ErrCAS) {
		t.Fatal("changed fixed HEAD accepted", err)
	}
	head, err = g.Head(context.Background(), repo)
	if err != nil || head != moved {
		t.Fatal("stale snapshot overwrote HEAD")
	}
	if _, err := os.Stat(filepath.Join(repo, "src/new.txt")); err != nil {
		t.Fatal("stale snapshot discarded new file")
	}
}

func TestCommitScopedRejectsLinkedMetadataBeforeAnyWrites(t *testing.T) {
	g, repo, base, scope := scopedFixture(t)
	writeFile(t, repo, "src/modify.txt", "private intended\n")
	external := filepath.Join(t.TempDir(), "external-objects")
	if err := os.Rename(filepath.Join(repo, ".git", "objects"), external); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("external must remain unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(repo, ".git", "objects")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("Windows symlink privilege unavailable")
		}
		t.Fatal(err)
	}
	before := map[string]string{}
	if err := filepath.WalkDir(external, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		before[path] = string(data)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.CommitScoped(context.Background(), repo, base, scope, "must refuse linked metadata", testIdentity); !errors.Is(err, ErrUnsupportedObject) {
		t.Fatal("linked metadata accepted", err)
	}
	after := map[string]string{}
	if err := filepath.WalkDir(external, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		after[path] = string(data)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) {
		t.Fatal("snapshot wrote objects to external repository")
	}
	for path, contents := range before {
		if after[path] != contents {
			t.Fatal("external metadata changed", path)
		}
	}
	head, err := g.Head(context.Background(), repo)
	if err != nil || head != base {
		t.Fatal("linked metadata refusal advanced HEAD", err)
	}
}

func TestCommitScopedReportsGeneratedDependencyInputWithoutDiscardingIt(t *testing.T) {
	g, repo, base, scope := scopedFixture(t)
	writeFile(t, repo, "src/modify.txt", "intended change\n")
	writeFile(t, repo, "cache/package/bin/tool", "dependency executable\n")
	link := filepath.Join(repo, "cache", "package", "tool")
	if err := os.Symlink("bin/tool", link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("Windows symlink privilege unavailable")
		}
		t.Fatal(err)
	}
	_, err := g.CommitScoped(context.Background(), repo, base, scope, "must preserve rejected inputs", testIdentity)
	if !errors.Is(err, ErrUnsupportedObject) || !strings.Contains(err.Error(), "cache/package/tool") {
		t.Fatalf("snapshot failure must identify the relative unsupported input: %v", err)
	}
	if head, headErr := g.Head(context.Background(), repo); headErr != nil || head != base {
		t.Fatalf("snapshot refusal changed fixed HEAD: %s %v", head, headErr)
	}
	if target, readErr := os.Readlink(link); readErr != nil || target != "bin/tool" {
		t.Fatalf("snapshot refusal discarded dependency inputs: %q %v", target, readErr)
	}
}
