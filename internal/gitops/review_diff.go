package gitops

import (
	"bytes"
	"context"
)

// DiffTextPreview omits binary encodings, not textual hunks. Binary paths are
// returned separately so a consumer must account for every omitted object.
func (g *Client) DiffTextPreview(ctx context.Context, repo string, from, to OID) ([]byte, []string, error) {
	if err := validIDs(from, to); err != nil {
		return nil, nil, err
	}
	if err := g.guardRepo(ctx, repo); err != nil {
		return nil, nil, err
	}
	for _, oid := range []OID{from, to} {
		if err := g.RequireCommit(ctx, repo, oid); err != nil {
			return nil, nil, err
		}
	}
	stat, err := g.run(ctx, repo, nil, "diff", "--numstat", "-z", "--no-renames", "--no-ext-diff", "--no-textconv", string(from), string(to), "--")
	if err != nil {
		return nil, nil, err
	}
	binary, err := binaryPathsInNumstat(stat)
	if err != nil {
		return nil, nil, err
	}
	patch, err := g.run(ctx, repo, nil, "diff", "--patch", "--full-index", "--no-renames", "--no-ext-diff", "--no-textconv", string(from), string(to), "--")
	return patch, binary, err
}

// Numstat is a Git object protocol: tabs may occur in its NUL-delimited
// paths even on hosts whose worktree filesystem cannot create those names.
func binaryPathsInNumstat(stat []byte) ([]string, error) {
	var binary []string
	for _, record := range bytes.Split(stat, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		fields := bytes.SplitN(record, []byte{'\t'}, 3)
		if len(fields) != 3 || ValidatePath(string(fields[2])) != nil {
			return nil, fault("diff", ErrGit, "malformed numstat record")
		}
		if string(fields[0]) == "-" || string(fields[1]) == "-" {
			if string(fields[0]) != "-" || string(fields[1]) != "-" {
				return nil, fault("diff", ErrGit, "inconsistent binary numstat")
			}
			binary = append(binary, string(fields[2]))
		}
	}
	return binary, nil
}

// ReadBlob reads a pinned object with the same managed output/stop bounds as
// other Git reads. It cannot run filters or interpret a repository path/ref.
func (g *Client) ReadBlob(ctx context.Context, repo string, oid OID) ([]byte, error) {
	if err := validIDs(oid); err != nil {
		return nil, err
	}
	if err := g.guardRepo(ctx, repo); err != nil {
		return nil, err
	}
	return g.run(ctx, repo, nil, "cat-file", "blob", string(oid))
}
