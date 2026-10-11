package gitops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// CommitScoped snapshots a quiescent, independently created managed worktree.
// It never resets or cleans files. Scope and complete physical inputs are
// checked with a private index before creating a commit or advancing HEAD.
// On success, updating that managed worktree's index is part of committing it;
// this operation must never be pointed at the user's original project checkout.
func (g *Client) CommitScoped(ctx context.Context, repo string, expected OID, scope *Scope, message string, id Identity) (OID, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if scope == nil {
		return "", fmt.Errorf("scope required")
	}
	if err := validIDs(expected); err != nil {
		return "", err
	}
	if err := g.guardRepo(ctx, repo); err != nil {
		return "", err
	}
	if err := g.snapshotRepository(ctx, repo); err != nil {
		return "", err
	}
	head, err := g.Head(ctx, repo)
	if err != nil {
		return "", err
	}
	if head != expected {
		return "", ErrCAS
	}
	if err := g.snapshotIndex(ctx, repo, expected); err != nil {
		return "", err
	}
	if err := walkSnapshotInputs(ctx, repo, nil); err != nil {
		return "", err
	}
	ignored, err := g.run(ctx, repo, nil, "ls-files", "--others", "--ignored", "--exclude-standard", "-z")
	if err != nil {
		return "", err
	}
	if len(ignored) != 0 {
		return "", fault("snapshot", ErrDirty, "ignored private inputs cannot be omitted")
	}
	if strings.ContainsRune(message, 0) || len(message) > 4096 {
		return "", fmt.Errorf("invalid snapshot commit message")
	}
	identity, err := identityEnv(id)
	if err != nil {
		return "", err
	}
	temp, err := os.MkdirTemp("", "coordica-scoped-index-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(temp)
	env := []string{"GIT_INDEX_FILE=" + filepath.Join(temp, "index")}
	if _, err = g.run(ctx, repo, env, "read-tree", string(expected)); err != nil {
		return "", err
	}
	add := []string{"add", "--all", "--", ":/"}
	if runtime.GOOS != "windows" {
		add = append([]string{"-c", "core.fileMode=true"}, add...)
	}
	if _, err = g.run(ctx, repo, env, add...); err != nil {
		return "", err
	}
	rawTree, err := g.run(ctx, repo, env, "write-tree")
	if err != nil {
		return "", err
	}
	tree, err := ParseOID(strings.TrimSpace(string(rawTree)))
	if err != nil {
		return "", err
	}
	rawDiff, err := g.run(ctx, repo, env, "diff", "--raw", "-z", "--no-abbrev", "--no-ext-diff", "--no-textconv", "--find-renames=50%", string(expected), string(tree), "--")
	if err != nil {
		return "", err
	}
	changes, err := parseRawDiff(rawDiff)
	if err != nil {
		return "", err
	}
	if len(changes) == 0 {
		return "", ErrNoChanges
	}
	var paths []string
	for _, change := range changes {
		paths = append(paths, change.Paths()...)
	}
	if !scope.Check(paths).Allowed {
		return "", ErrOutOfScope
	}
	for _, change := range changes {
		if change.Unsupported() {
			return "", ErrUnsupportedObject
		}
	}
	if err = g.verifySnapshotInputs(ctx, repo, tree); err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	// Recheck the original index and HEAD after staging; a concurrent actor may
	// not hide input changes or have its staged state overwritten by the snapshot.
	if err = g.snapshotIndex(ctx, repo, expected); err != nil {
		return "", err
	}
	head, err = g.Head(ctx, repo)
	if err != nil {
		return "", err
	}
	if head != expected {
		return "", ErrCAS
	}
	out, err := g.run(ctx, repo, identity, "commit-tree", string(tree), "-p", string(expected), "-m", message)
	if err != nil {
		return "", err
	}
	commit, err := ParseOID(strings.TrimSpace(string(out)))
	if err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if _, err = g.run(ctx, repo, nil, "update-ref", "--no-deref", "HEAD", string(commit), string(expected)); err != nil {
		if ctx.Err() != nil {
			return "", errors.Join(ctx.Err(), err)
		}
		return "", fault("snapshot", errors.Join(ErrCAS, err), "HEAD changed before snapshot publication")
	}
	// No file checkout occurs. If this final synchronization or verification
	// fails, the private snapshot remains for diagnosis and the caller must not
	// upload an artifact or report this Run as successful.
	if _, err = g.run(ctx, repo, nil, "read-tree", string(commit)); err != nil {
		return "", err
	}
	if err = g.VerifyClean(ctx, repo, commit); err != nil {
		return "", err
	}
	return commit, nil
}

func (g *Client) snapshotRepository(ctx context.Context, repo string) error {
	root, err := filepath.Abs(repo)
	if err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	metadata, err := os.Lstat(filepath.Join(root, ".git"))
	if err != nil || !metadata.IsDir() || metadata.Mode()&os.ModeSymlink != 0 {
		return fault("snapshot", ErrUnsupportedObject, "an independent managed repository is required")
	}
	if err := verifySnapshotMetadata(ctx, filepath.Join(root, ".git")); err != nil {
		return err
	}
	out, err := g.run(ctx, repo, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	actual, err := filepath.EvalSymlinks(strings.TrimSpace(string(out)))
	if err != nil || !sameSnapshotPath(actual, root) {
		return fault("snapshot", ErrUnsupportedObject, "repository worktree differs from fixed directory")
	}
	out, err = g.run(ctx, repo, nil, "rev-parse", "--git-dir")
	if err != nil {
		return err
	}
	gitDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(root, gitDir)
	}
	if !sameSnapshotPath(gitDir, filepath.Join(root, ".git")) {
		return fault("snapshot", ErrUnsupportedObject, "external Git metadata is unsupported")
	}
	out, err = g.run(ctx, repo, nil, "rev-parse", "--git-common-dir")
	if err != nil {
		return err
	}
	common := strings.TrimSpace(string(out))
	if !filepath.IsAbs(common) {
		common = filepath.Join(root, common)
	}
	if !sameSnapshotPath(common, filepath.Join(root, ".git")) {
		return fault("snapshot", ErrUnsupportedObject, "external shared Git metadata is unsupported")
	}
	return nil
}

func verifySnapshotMetadata(ctx context.Context, metadata string) error {
	entries := 0
	return filepath.WalkDir(metadata, func(path string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		entries++
		if entries > 100000 {
			return ErrOutputLimit
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fault("snapshot", ErrUnsupportedObject, "linked or special Git metadata is unsupported")
		}
		relative, err := filepath.Rel(metadata, path)
		if err != nil {
			return err
		}
		switch filepath.ToSlash(relative) {
		case "objects/info/alternates", "objects/info/http-alternates", "commondir", "info/grafts":
			return fault("snapshot", ErrUnsupportedObject, "external or rewritten Git metadata is unsupported")
		}
		return nil
	})
}

func (g *Client) snapshotIndex(ctx context.Context, repo string, expected OID) error {
	for _, flag := range []string{"-v", "-f"} {
		out, err := g.run(ctx, repo, nil, "ls-files", flag, "-z")
		if err != nil {
			return err
		}
		for _, record := range bytes.Split(out, []byte{0}) {
			if len(record) != 0 && (len(record) < 3 || record[0] != 'H') {
				return fault("snapshot", ErrDirty, "unsupported or hidden index flags")
			}
		}
	}
	if _, err := g.run(ctx, repo, nil, "diff-index", "--cached", "--quiet", string(expected), "--"); err != nil {
		return fault("snapshot", errors.Join(ErrDirty, err), "the managed index differs from fixed HEAD")
	}
	return nil
}

// verifySnapshotInputs reuses the same raw blob/mode check as VerifyClean,
// additionally walking real inputs so ignored files and hidden untracked paths
// cannot disappear through index flags, ignore rules, or nested repositories.
func (g *Client) verifySnapshotInputs(ctx context.Context, repo string, tree OID) error {
	out, err := g.run(ctx, repo, nil, "ls-tree", "-r", "--full-tree", "-z", string(tree))
	if err != nil {
		return err
	}
	files := map[string]bool{}
	for _, record := range bytes.Split(out, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		pair := bytes.SplitN(record, []byte{'\t'}, 2)
		if len(pair) != 2 {
			return ErrGit
		}
		fields := strings.Fields(string(pair[0]))
		if len(fields) != 3 {
			return ErrGit
		}
		if (fields[0] != "100644" && fields[0] != "100755") || fields[1] != "blob" {
			return ErrUnsupportedObject
		}
		name := string(pair[1])
		if err := ValidatePath(name); err != nil {
			return ErrUnsupportedObject
		}
		if err := verifyBlob(filepath.Join(repo, filepath.FromSlash(name)), fields[2], fields[0]); err != nil {
			return err
		}
		files[name] = true
	}
	return walkSnapshotInputs(ctx, repo, files)
}

func walkSnapshotInputs(ctx context.Context, repo string, files map[string]bool) error {
	return filepath.WalkDir(repo, func(path string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(repo, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		name := filepath.ToSlash(relative)
		if name == ".git" {
			return filepath.SkipDir
		}
		for _, component := range strings.Split(name, "/") {
			if component == ".git" {
				return ErrUnsupportedObject
			}
		}
		if err := ValidatePath(name); err != nil {
			return ErrUnsupportedObject
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return ErrUnsupportedObject
		}
		if files != nil && !files[name] {
			return fault("snapshot", ErrDirty, "private input absent from fixed snapshot")
		}
		return nil
	})
}

func sameSnapshotPath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
