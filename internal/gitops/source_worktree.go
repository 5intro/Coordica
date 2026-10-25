package gitops

import (
	"context"
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
	out, err := r.Git.run(ctx, repo, nil, "ls-files", "-v", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return err
	}
	paths := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	if len(paths) > 20000 {
		return ErrOutputLimit
	}
	total := 0
	for _, entry := range paths {
		if entry == "" {
			continue
		}
		if len(entry) < 3 || entry[1] != ' ' {
			return ErrUnsupportedObject
		}
		name, sparse := entry[2:], entry[0] == 'S' || entry[0] == 's'
		if !include(name) {
			continue
		}
		if ValidatePath(name) != nil || name == "." {
			return ErrUnsupportedObject
		}
		data, exists, err := r.localSource(ctx, repo, name, sparse)
		if err != nil {
			return err
		}
		if !exists {
			continue
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
