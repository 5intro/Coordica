package gitops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Worktree is only for the local developer check. Center integration always
// uses Read on immutable objects. Tracked files cannot hide in .gitignore.
func (r SourceReader) Worktree(
	ctx context.Context, repo string,
	include func(string) bool, consume func(string, string) error,
) error {
	if err := r.Git.guardRepo(ctx, repo); err != nil {
		return err
	}
	out, err := r.Git.run(ctx, repo, nil, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return err
	}
	paths := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	if len(paths) > 20000 {
		return ErrOutputLimit
	}
	total := 0
	for _, name := range paths {
		if name == "" || !include(name) {
			continue
		}
		if ValidatePath(name) != nil || name == "." {
			return ErrUnsupportedObject
		}
		file := filepath.Join(repo, filepath.FromSlash(name))
		info, err := os.Lstat(file)
		if errors.Is(err, os.ErrNotExist) {
			continue // Locally deleted source.
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return ErrUnsupportedObject
		}
		if info.Size() > 2<<20 {
			return ErrOutputLimit
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		total += len(data)
		if len(data) > 2<<20 || total > 64<<20 {
			return ErrOutputLimit
		}
		if err = consume(name, string(data)); err != nil {
			return err
		}
	}
	return nil
}
