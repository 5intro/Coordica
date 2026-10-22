package gitops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (r SourceReader) localSource(ctx context.Context, repo, name string, sparse bool) ([]byte, bool, error) {
	file := filepath.Join(repo, filepath.FromSlash(name))
	info, err := os.Lstat(file)
	if errors.Is(err, os.ErrNotExist) {
		if !sparse {
			return nil, false, nil // A real local deletion, not an out-of-cone entry.
		}
		data, err := r.indexSource(ctx, repo, name)
		return data, true, err
	}
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, ErrUnsupportedObject
	}
	if info.Size() > 2<<20 {
		return nil, false, ErrOutputLimit
	}
	data, err := os.ReadFile(file)
	return data, true, err
}

// The skip-worktree bit means absent tracked content still exists in the
// index. Pin its blob before reading; do not substitute HEAD for staged edits.
func (r SourceReader) indexSource(ctx context.Context, repo, name string) ([]byte, error) {
	out, err := r.Git.run(ctx, repo, nil, "ls-files", "--stage", "-z", "--", name)
	if err != nil {
		return nil, err
	}
	header, path, ok := strings.Cut(strings.TrimSuffix(string(out), "\x00"), "\t")
	fields := strings.Fields(header)
	if !ok || path != name || len(fields) != 3 || fields[2] != "0" || fields[0] != "100644" && fields[0] != "100755" {
		return nil, ErrUnsupportedObject
	}
	if err = validIDs(OID(fields[1])); err != nil {
		return nil, err
	}
	out, err = r.Git.run(ctx, repo, nil, "cat-file", "-s", fields[1])
	if err != nil {
		return nil, err
	}
	size, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || size < 0 || size > 2<<20 {
		return nil, ErrOutputLimit
	}
	return r.Git.run(ctx, repo, nil, "cat-file", "blob", fields[1])
}
