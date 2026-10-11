package gitops

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var testIdentity = Identity{Name: "Coordica Test", Email: "test@example.invalid"}

func gitClient(t *testing.T) *Client {
	t.Helper()
	binary, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git unavailable")
	}
	return New(binary)
}
func mustRun(t *testing.T, g *Client, repo string, args ...string) []byte {
	t.Helper()
	out, err := g.run(context.Background(), repo, nil, args...)
	if err != nil {
		t.Fatalf("%v: %v", args, err)
	}
	return out
}
func writeFile(t *testing.T, repo, name, text string) {
	t.Helper()
	file := filepath.Join(repo, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func commit(t *testing.T, g *Client, repo, msg string) OID {
	t.Helper()
	oid, err := g.CommitAll(context.Background(), repo, msg, testIdentity)
	if err != nil {
		t.Fatal(err)
	}
	return oid
}
func newRepo(t *testing.T, g *Client) (string, OID) {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "source repo")
	if err := g.Init(context.Background(), repo, false); err != nil {
		t.Fatal(err)
	}
	writeFile(t, repo, "src/main.txt", "base\n")
	writeFile(t, repo, "src/delete.txt", "delete\n")
	writeFile(t, repo, ".gitignore", "cache/\n")
	return repo, commit(t, g, repo, "base")
}

func TestFullOIDValidation(t *testing.T) {
	for _, s := range []string{"HEAD", "abc", "-a", strings.Repeat("z", 40), strings.Repeat("0", 40), strings.Repeat("a", 39)} {
		if _, err := ParseOID(s); !errors.Is(err, ErrInvalidOID) {
			t.Fatalf("accepted %q", s)
		}
	}
	for _, size := range []int{40, 64} {
		got, err := ParseOID(strings.Repeat("A", size))
		if err != nil || got != OID(strings.Repeat("a", size)) {
			t.Fatalf("parse full ID: %q %v", got, err)
		}
	}
}

func TestDiffPatchCompleteBinaryAndExplicitOutputLimit(t *testing.T) {
	ctx := context.Background()
	g := gitClient(t)
	repo, b := newRepo(t, g)
	writeFile(t, repo, "src/binary.bin", "\x00\x01\xffbinary\n")
	h := commit(t, g, repo, "binary material")
	patch, err := g.DiffPatch(ctx, repo, b, h)
	if err != nil || !strings.Contains(string(patch), "GIT binary patch") || !strings.Contains(string(patch), "src/binary.bin") {
		t.Fatalf("binary material absent from complete patch: %s %v", patch, err)
	}
	// A review must block on oversize, never approve silently truncated input.
	writeFile(t, repo, "src/large.txt", strings.Repeat("large review input\n", maxOutput/18+1))
	large := commit(t, g, repo, "oversize review input")
	patch, err = g.DiffPatch(ctx, repo, b, large)
	if !errors.Is(err, ErrOutputLimit) {
		t.Fatalf("oversize review patch did not fail explicitly: length=%d: %.300s: %v", len(patch), patch, err)
	}
}

func TestVerifyCleanRejectsExecutableModeHiddenByConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not represent Git executable mode")
	}
	g := gitClient(t)
	repo, b := newRepo(t, g)
	mustRun(t, g, repo, "config", "core.fileMode", "false")
	if err := os.Chmod(filepath.Join(repo, "src", "main.txt"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := g.VerifyClean(context.Background(), repo, b); !errors.Is(err, ErrDirty) {
		t.Fatalf("hidden executable change passed fixed C gate: %v", err)
	}
}

func TestBundleCandidateAndExactCAS(t *testing.T) {
	ctx := context.Background()
	g := gitClient(t)
	source, b := newRepo(t, g)
	center := filepath.Join(t.TempDir(), "center")
	if err := g.Clone(ctx, source, center); err != nil {
		t.Fatal(err)
	}
	if err := g.Checkout(ctx, center, b); err != nil {
		t.Fatal(err)
	}
	writeFile(t, source, "src/main.txt", "task implementation\n")
	h := commit(t, g, source, "task")
	bundlePath := filepath.Join(t.TempDir(), "head.bundle")
	material, err := g.CreateBundle(ctx, source, b, h, bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := g.ImportBundle(ctx, center, bundlePath, b, h)
	if err != nil {
		t.Fatal(err)
	}
	if material.SHA256 != imported.SHA256 || material.Bytes != imported.Bytes || material.Ref != imported.Ref {
		t.Fatalf("material identity changed: %+v %+v", material, imported)
	}
	writeFile(t, center, "src/other.txt", "main advance\n")
	m := commit(t, g, center, "advance main")
	workspace := filepath.Join(t.TempDir(), "candidate")
	candidate, err := g.BuildCandidate(ctx, center, m, h, workspace, testIdentity)
	if err != nil {
		t.Fatal(err)
	}
	parents := strings.Fields(string(mustRun(t, g, center, "rev-list", "--parents", "-n", "1", string(candidate.Commit))))
	if len(parents) != 3 || parents[1] != string(m) || parents[2] != string(h) {
		t.Fatalf("candidate parents %v", parents)
	}
	if err = g.VerifyClean(ctx, workspace, candidate.Commit); err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(t.TempDir(), "remote.git")
	if err = g.Init(ctx, remote, true); err != nil {
		t.Fatal(err)
	}
	mustRun(t, g, center, "push", "--", remote, string(m)+":refs/heads/main")
	result, err := g.Publish(ctx, center, remote, "refs/heads/main", m, candidate.Commit)
	if err != nil {
		t.Fatal(err)
	}
	if result.Observed != candidate.Commit {
		t.Fatalf("published different commit: %+v", result)
	}
	observed, err := g.Query(ctx, remote, "refs/heads/main")
	if err != nil || observed != candidate.Commit {
		t.Fatalf("ACK reconciliation %q %v", observed, err)
	}
	// The target contains the exact already-verified commit, including parents.
	remoteParents := strings.Fields(string(mustRun(t, g, remote, "rev-list", "--parents", "-n", "1", string(observed))))
	if strings.Join(remoteParents, " ") != strings.Join(parents, " ") {
		t.Fatal("remote regenerated candidate")
	}
	// A successful old intent is observed by Query, never blindly republished.
	if _, err = g.Publish(ctx, center, remote, "refs/heads/main", m, candidate.Commit); !errors.Is(err, ErrCAS) {
		t.Fatalf("old expected main accepted: %v", err)
	}
}

func TestSnapshotBundleAndFreshFetch(t *testing.T) {
	ctx := context.Background()
	g := gitClient(t)
	source, b := newRepo(t, g)
	file := filepath.Join(t.TempDir(), "snapshot.bundle")
	snap, err := g.CreateSnapshotBundle(ctx, source, b, file)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Base != b || snap.Head != b {
		t.Fatalf("snapshot metadata %+v", snap)
	}
	workspace := filepath.Join(t.TempDir(), "runner")
	if err = g.CloneSnapshot(ctx, file, b, workspace); err != nil {
		t.Fatal(err)
	}
	if err = g.VerifyClean(ctx, workspace, b); err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(t.TempDir(), "remote.git")
	if err = g.Init(ctx, remote, true); err != nil {
		t.Fatal(err)
	}
	mustRun(t, g, source, "push", "--", remote, string(b)+":refs/heads/main")
	writeFile(t, source, "src/later.txt", "later\n")
	later := commit(t, g, source, "later")
	mustRun(t, g, source, "push", "--", remote, string(later)+":refs/heads/main")
	got, err := g.Fetch(ctx, workspace, remote, "refs/heads/main")
	if err != nil || got != later {
		t.Fatalf("fetch used stale clone main %q %v", got, err)
	}
	if err = g.requireCommit(ctx, workspace, later); err != nil {
		t.Fatal("fetch did not retain objects", err)
	}
}

func TestScopeRenameDeleteAndSpecialNames(t *testing.T) {
	ctx := context.Background()
	g := gitClient(t)
	repo, b := newRepo(t, g)
	if err := os.Rename(filepath.Join(repo, "src/main.txt"), filepath.Join(repo, "outside.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo, "src/delete.txt")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, repo, "src/空 格.txt", "unicode\n")
	if runtime.GOOS != "windows" {
		writeFile(t, repo, "src/tab\tline\n.txt", "special\n")
	}
	h := commit(t, g, repo, "rename and delete")
	changes, err := g.DiffPaths(ctx, repo, b, h)
	if err != nil {
		t.Fatal(err)
	}
	var rename, deleted, unicode bool
	for _, c := range changes {
		if c.Status == "R" && c.OldPath == "src/main.txt" && c.NewPath == "outside.txt" {
			rename = true
		}
		if c.Status == "D" && c.OldPath == "src/delete.txt" && c.NewPath == "" {
			deleted = true
		}
		if c.NewPath == "src/空 格.txt" {
			unicode = true
		}
	}
	if !rename || !deleted || !unicode {
		t.Fatalf("raw changes lost paths: %+v", changes)
	}
	scope, err := NewScope([]string{"src/"}, []string{"outside.txt"})
	if err != nil {
		t.Fatal(err)
	}
	checked, err := g.CheckScope(ctx, repo, b, h, scope)
	if !errors.Is(err, ErrOutOfScope) || len(checked.Scope.OutOfScope) != 1 || checked.Scope.OutOfScope[0] != "outside.txt" || len(checked.Scope.Protected) != 1 {
		t.Fatalf("rename destination bypassed scope: %+v %v", checked, err)
	}
}

func TestSymlinkRejected(t *testing.T) {
	ctx := context.Background()
	g := gitClient(t)
	repo, b := newRepo(t, g)
	if err := os.Symlink("main.txt", filepath.Join(repo, "src/link")); err != nil {
		t.Skip("symlink unavailable:", err)
	}
	h := commit(t, g, repo, "symlink")
	scope, err := NewScope([]string{"src/"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	checked, err := g.CheckScope(ctx, repo, b, h, scope)
	if !errors.Is(err, ErrUnsupportedObject) || len(checked.Unsupported) != 1 || checked.Unsupported[0].NewMode != "120000" {
		t.Fatalf("symlink invisible: %+v %v", checked, err)
	}
}

func TestMainMovementRefusesCAS(t *testing.T) {
	ctx := context.Background()
	g := gitClient(t)
	repo, b := newRepo(t, g)
	writeFile(t, repo, "src/task.txt", "task\n")
	h := commit(t, g, repo, "task")
	workspace := filepath.Join(t.TempDir(), "candidate")
	candidate, err := g.BuildCandidate(ctx, repo, b, h, workspace, testIdentity)
	if err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(t.TempDir(), "remote.git")
	if err = g.Init(ctx, remote, true); err != nil {
		t.Fatal(err)
	}
	mustRun(t, g, repo, "push", "--", remote, string(b)+":refs/heads/main")
	if err = g.Checkout(ctx, repo, b); err != nil {
		t.Fatal(err)
	}
	writeFile(t, repo, "src/mainmove.txt", "other author\n")
	moved := commit(t, g, repo, "concurrent main")
	mustRun(t, g, repo, "push", "--", remote, string(moved)+":refs/heads/main")
	result, err := g.Publish(ctx, repo, remote, "refs/heads/main", b, candidate.Commit)
	if !errors.Is(err, ErrCAS) || result.Observed != moved {
		t.Fatalf("moved main overwritten: %+v %v", result, err)
	}
	actual, err := g.Query(ctx, remote, "refs/heads/main")
	if err != nil || actual != moved {
		t.Fatalf("unexpected remote %q %v", actual, err)
	}
	if _, err = g.Publish(ctx, repo, remote, "refs/heads/main", h, b); !errors.Is(err, ErrNotAncestor) {
		t.Fatalf("rewriting history allowed: %v", err)
	}
}

func TestLeaseRejectsMoveBetweenQueryAndPush(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX command interception; native Windows CAS covered by the shared bare-remote tests")
	}
	ctx := context.Background()
	g := gitClient(t)
	repo, b := newRepo(t, g)
	writeFile(t, repo, "src/task.txt", "task\n")
	h := commit(t, g, repo, "task")
	candidate, err := g.BuildCandidate(ctx, repo, b, h, filepath.Join(t.TempDir(), "candidate"), testIdentity)
	if err != nil {
		t.Fatal(err)
	}
	if err = g.Checkout(ctx, repo, b); err != nil {
		t.Fatal(err)
	}
	writeFile(t, repo, "src/concurrent.txt", "concurrent\n")
	moved := commit(t, g, repo, "concurrent")
	remote := filepath.Join(t.TempDir(), "remote.git")
	if err = g.Init(ctx, remote, true); err != nil {
		t.Fatal(err)
	}
	mustRun(t, g, repo, "push", "--", remote, string(b)+":refs/heads/main")
	// Transfer D's object without changing main, then change main exactly when
	// Publish invokes push, after its initial Query observed B.
	mustRun(t, g, repo, "push", "--", remote, string(moved)+":refs/heads/other")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	script := "#!/bin/sh\nfor arg do\n if [ \"$arg\" = push ]; then\n " + quote(g.GitPath) + " -C " + quote(remote) + " update-ref refs/heads/main " + quote(string(moved)) + " " + quote(string(b)) + " || exit 90\n break\n fi\ndone\nexec " + quote(g.GitPath) + " \"$@\"\n"
	wrapper := filepath.Join(t.TempDir(), "git-intercept")
	if err = os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	intercept := New(wrapper)
	result, err := intercept.Publish(ctx, repo, remote, "refs/heads/main", b, candidate.Commit)
	if !errors.Is(err, ErrCAS) || result.Observed != moved {
		t.Fatalf("lease did not reject real race: %+v %v", result, err)
	}
	actual, err := g.Query(ctx, remote, "refs/heads/main")
	if err != nil || actual != moved {
		t.Fatalf("race overwrote main %q %v", actual, err)
	}
}

func TestCleanVerificationRejectsExtraAndHiddenTrackedChanges(t *testing.T) {
	ctx := context.Background()
	g := gitClient(t)
	repo, b := newRepo(t, g)
	if err := g.VerifyClean(ctx, repo, b); err != nil {
		t.Fatal(err)
	}
	writeFile(t, repo, "cache/injected.txt", "ignored extra\n")
	if err := g.VerifyClean(ctx, repo, b); !errors.Is(err, ErrDirty) {
		t.Fatalf("ignored input missed: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(repo, "cache")); err != nil {
		t.Fatal(err)
	}
	mustRun(t, g, repo, "update-index", "--assume-unchanged", "--", "src/main.txt")
	writeFile(t, repo, "src/main.txt", "hidden tracked modification\n")
	if err := g.VerifyClean(ctx, repo, b); !errors.Is(err, ErrDirty) {
		t.Fatalf("assume-unchanged hid mutation: %v", err)
	}
}

func TestHooksDoNotExecute(t *testing.T) {
	ctx := context.Background()
	g := gitClient(t)
	repo, b := newRepo(t, g)
	marker := filepath.Join(t.TempDir(), "HOOK_EXECUTED")
	script := []byte("#!/bin/sh\nprintf executed > '" + strings.ReplaceAll(marker, "'", "'\"'\"'") + "'\nexit 94\n")
	for _, hook := range []string{"pre-commit", "commit-msg", "post-commit", "post-checkout", "pre-merge-commit", "pre-push"} {
		if err := os.WriteFile(filepath.Join(repo, ".git", "hooks", hook), script, 0700); err != nil {
			if os.IsNotExist(err) {
				if err = os.MkdirAll(filepath.Join(repo, ".git", "hooks"), 0700); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(repo, ".git", "hooks", hook), script, 0700); err != nil {
					t.Fatal(err)
				}
			} else {
				t.Fatal(err)
			}
		}
	}
	writeFile(t, repo, "src/task.txt", "task\n")
	h := commit(t, g, repo, "task with disabled hooks")
	candidate, err := g.BuildCandidate(ctx, repo, b, h, filepath.Join(t.TempDir(), "candidate"), testIdentity)
	if err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(t.TempDir(), "remote.git")
	if err = g.Init(ctx, remote, true); err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Join(remote, "hooks"), 0700); err != nil {
		t.Fatal(err)
	}
	mustRun(t, g, repo, "push", "--", remote, string(b)+":refs/heads/main")
	if _, err = g.Publish(ctx, repo, remote, "refs/heads/main", b, candidate.Commit); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("Git hook executed", err)
	}
}

func TestRemotePolicyIsNotBypassed(t *testing.T) {
	ctx := context.Background()
	g := gitClient(t)
	repo, b := newRepo(t, g)
	remote := filepath.Join(t.TempDir(), "remote.git")
	if err := g.Init(ctx, remote, true); err != nil {
		t.Fatal(err)
	}
	mustRun(t, g, repo, "push", "--", remote, string(b)+":refs/heads/main")
	writeFile(t, repo, "src/task.txt", "task\n")
	h := commit(t, g, repo, "task")
	if err := os.MkdirAll(filepath.Join(remote, "hooks"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remote, "hooks", "pre-receive"), []byte("#!/bin/sh\nexit 94\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Publish(ctx, repo, remote, "refs/heads/main", b, h); !errors.Is(err, ErrGit) {
		t.Fatalf("server policy bypassed: %v", err)
	}
	actual, err := g.Query(ctx, remote, "refs/heads/main")
	if err != nil || actual != b {
		t.Fatalf("rejected push changed main %q %v", actual, err)
	}
}

func TestBundleRejectsWrongHeadAndCorruption(t *testing.T) {
	ctx := context.Background()
	g := gitClient(t)
	repo, b := newRepo(t, g)
	center := filepath.Join(t.TempDir(), "center")
	if err := g.Clone(ctx, repo, center); err != nil {
		t.Fatal(err)
	}
	writeFile(t, repo, "src/task.txt", "task\n")
	h := commit(t, g, repo, "task")
	bundle := filepath.Join(t.TempDir(), "material.bundle")
	if _, err := g.CreateBundle(ctx, repo, b, h, bundle); err != nil {
		t.Fatal(err)
	}
	if _, err := g.ImportBundle(ctx, center, bundle, b, b); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("wrong fixed H accepted: %v", err)
	}
	corrupt := filepath.Join(t.TempDir(), "bad.bundle")
	if err := os.WriteFile(corrupt, []byte("not a Git bundle"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := g.ImportBundle(ctx, center, corrupt, b, h); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("corrupt material accepted: %v", err)
	}
}

func TestMergeConflictHasTypedError(t *testing.T) {
	ctx := context.Background()
	g := gitClient(t)
	repo, b := newRepo(t, g)
	writeFile(t, repo, "src/main.txt", "task edit\n")
	h := commit(t, g, repo, "task edit")
	if err := g.Checkout(ctx, repo, b); err != nil {
		t.Fatal(err)
	}
	writeFile(t, repo, "src/main.txt", "main edit\n")
	m := commit(t, g, repo, "main edit")
	if _, err := g.BuildCandidate(ctx, repo, m, h, filepath.Join(t.TempDir(), "conflict"), testIdentity); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict lost: %v", err)
	}
}

func TestUnsafeGitConfigRejected(t *testing.T) {
	ctx := context.Background()
	g := gitClient(t)
	repo, b := newRepo(t, g)
	for _, key := range []string{"filter.inject.clean", "merge.inject.driver", "include.path", "diff.inject.textconv", "uploadpack.packObjectsHook"} {
		t.Run(key, func(t *testing.T) {
			mustRun(t, g, repo, "config", "--local", key, "must-not-execute")
			defer mustRun(t, g, repo, "config", "--local", "--unset", key)
			if _, err := g.DiffPaths(ctx, repo, b, b); !errors.Is(err, ErrUnsafeConfig) {
				t.Fatalf("unsafe config %s accepted: %v", key, err)
			}
		})
	}
}

func TestHTTPSCredentialBindingAndInputValidation(t *testing.T) {
	g := gitClient(t)
	remote := "https://github.com/owner/repo.git"
	auth, err := g.WithHTTPSAuthorization(remote, "Bearer secret-test-value")
	if err != nil {
		t.Fatal(err)
	}
	if len(g.authEnv(remote)) != 0 || len(auth.authEnv("https://github.com/other/repo.git")) != 0 {
		t.Fatal("authorization escaped configured client/URL")
	}
	env := controlledEnv(auth.authEnv(remote))
	var found bool
	for _, e := range env {
		if strings.HasPrefix(e, "GIT_CONFIG_VALUE_") && strings.HasSuffix(e, "Authorization: Bearer secret-test-value") {
			found = true
		}
	}
	if !found {
		t.Fatal("authorization missing controlled environment")
	}
	for _, url := range []string{"http://github.com/a", "ssh://github.com/a", "https://user:pass@github.com/a", "https://github.com/a?token=x", "https://github.com/a#fragment", "https://github.com/a\nother", "file:///tmp/repo"} {
		if _, err := g.WithHTTPSAuthorization(url, "Bearer value"); err == nil {
			t.Fatalf("unsafe URL accepted %q", url)
		}
	}
	if _, err := g.WithHTTPSAuthorization(remote, "value\r\nextra: header"); err == nil {
		t.Fatal("header injection accepted")
	}
}

func TestContextCancellation(t *testing.T) {
	g := gitClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := g.Init(ctx, filepath.Join(t.TempDir(), "cancelled"), false); !errors.Is(err, context.Canceled) {
		t.Fatalf("context cancellation lost: %v", err)
	}
}

func TestSHA256Snapshot(t *testing.T) {
	ctx := context.Background()
	g := gitClient(t)
	repo := filepath.Join(t.TempDir(), "sha256")
	if _, err := g.run(ctx, "", nil, "init", "--template=", "--initial-branch=main", "--object-format=sha256", "--", repo); err != nil {
		t.Skip("installed Git lacks SHA-256 repositories:", err)
	}
	writeFile(t, repo, "source.txt", "sha256 body\n")
	h := commit(t, g, repo, "sha256")
	if len(h) != 64 {
		t.Fatal("unexpected hash format", h)
	}
	file := filepath.Join(t.TempDir(), "sha256.bundle")
	if _, err := g.CreateSnapshotBundle(ctx, repo, h, file); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "clone")
	if err := g.CloneSnapshot(ctx, file, h, dest); err != nil {
		t.Fatal(err)
	}
	if err := g.VerifyClean(ctx, dest, h); err != nil {
		t.Fatal(err)
	}
}
