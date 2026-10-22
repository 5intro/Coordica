package gitops

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

type SourceReader struct{ Git *Client }

// SourceInventoryError describes immutable tree content outside reader bounds.
// Command failures, cancellation and unavailable repositories keep their own
// errors so consumers can distinguish source defects from operational faults.
type SourceInventoryError struct{ Cause error }

func (e *SourceInventoryError) Error() string {
	return "immutable source inventory: " + e.Cause.Error()
}
func (e *SourceInventoryError) Unwrap() error { return e.Cause }

// Read reads immutable blobs selected by a trusted caller. It does not
// interpret repository exclusions, attributes, hooks or generated-file prose.
// The callback receives at most 2 MiB per blob and 64 MiB across the tree.
func (r SourceReader) Read(
	ctx context.Context, repo string, commit OID,
	include func(string) bool, consume func(string, string) error,
) error {
	g := r.Git
	if g == nil {
		return errors.New("source reader requires a configured Git client")
	}
	if err := validIDs(commit); err != nil {
		return err
	}
	if err := g.guardRepo(ctx, repo); err != nil {
		return err
	}
	out, err := g.run(ctx, repo, nil, "ls-tree", "-r", "-l", "-z", string(commit))
	if err != nil {
		return err
	}
	entries := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	if len(entries) > 20000 {
		return &SourceInventoryError{Cause: ErrOutputLimit}
	}
	total := 0
	for _, entry := range entries {
		if entry == "" {
			continue
		}
		header, path, ok := strings.Cut(entry, "\t")
		fields := strings.Fields(header)
		if !ok || len(fields) != 4 || ValidatePath(path) != nil || path == "." {
			return &SourceInventoryError{Cause: ErrUnsupportedObject}
		}
		if !include(path) {
			continue
		}
		if fields[0] != "100644" && fields[0] != "100755" || fields[1] != "blob" {
			return &SourceInventoryError{Cause: ErrUnsupportedObject}
		}
		size, err := strconv.Atoi(fields[3])
		if err != nil || size < 0 || size > 2<<20 || total+size > 64<<20 {
			return &SourceInventoryError{Cause: ErrOutputLimit}
		}
		blob, err := g.run(ctx, repo, nil, "cat-file", "blob", fields[2])
		if err != nil {
			return err
		}
		total += len(blob)
		if len(blob) > 2<<20 || total > 64<<20 {
			return &SourceInventoryError{Cause: ErrOutputLimit}
		}
		if err := consume(path, string(blob)); err != nil {
			return errors.Join(errors.New("immutable source consumer rejected "+path), err)
		}
	}
	return nil
}
