package gitops

import (
	"context"
	"errors"

	"coordica/internal/process"
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
	out, err := readSourceManifest(ctx, g, repo, commit)
	if err != nil {
		return err
	}
	entries, err := sourceManifest(string(out), include)
	if err != nil {
		return err
	}
	return r.readSourceBatch(ctx, repo, entries, consume)
}

// 20,000 entries with typical OS path maxima plus object headers fit within
// this explicit bound. Larger immutable manifests are deterministic defects.
const maxSourceManifestBytes = 96 << 20

func readSourceManifest(ctx context.Context, g *Client, repo string, commit OID) ([]byte, error) {
	spec, err := gitCommandSpec(g, repo, nil, "ls-tree", "-r", "-l", "-z", string(commit))
	if err != nil {
		return nil, err
	}
	out, stderr, result, err := process.CollectRaw(ctx, spec, maxSourceManifestBytes, 64<<10)
	err = gitCommandFailure(ctx, "ls-tree", stderr, result, err)
	if ctx.Err() == nil && len(out) == maxSourceManifestBytes && errors.Is(err, ErrOutputLimit) &&
		!errors.Is(err, process.ErrTreeNotStopped) {
		return nil, &SourceInventoryError{Cause: err}
	}
	return out, err
}
