package gitops

import (
	"context"
	"errors"
	"strings"

	"coordica/internal/process"
)

func (g *Client) Head(ctx context.Context, repo string) (OID, error) {
	if err := g.guardRepo(ctx, repo); err != nil {
		return "", err
	}
	out, err := g.run(ctx, repo, nil, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return "", err
	}
	return ParseOID(strings.TrimSpace(string(out)))
}

// Checkout places a fixed commit in a managed workspace without discarding
// local modifications. Clone deliberately leaves checkout to this operation.
func (g *Client) Checkout(ctx context.Context, repo string, commit OID) error {
	if err := g.guardRepo(ctx, repo); err != nil {
		return err
	}
	if err := g.RequireCommit(ctx, repo, commit); err != nil {
		return err
	}
	_, err := g.run(ctx, repo, nil, "checkout", "--detach", string(commit))
	return err
}

// RequireCommit accepts an exact available commit object, never a tag or revision.
func (g *Client) RequireCommit(ctx context.Context, repo string, o OID) error {
	if err := validIDs(o); err != nil {
		return err
	}
	if err := g.guardRepo(ctx, repo); err != nil {
		return err
	}
	out, err := g.run(ctx, repo, nil, "rev-parse", "--verify", string(o)+"^{commit}")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(out)) != string(o) {
		return fault("rev-parse", ErrUnsupportedObject, "exact full commit object required")
	}
	return nil
}

func (g *Client) IsAncestor(ctx context.Context, repo string, from, to OID) (bool, error) {
	if err := validIDs(from, to); err != nil {
		return false, err
	}
	if err := g.guardRepo(ctx, repo); err != nil {
		return false, err
	}
	for _, o := range []OID{from, to} {
		if err := g.RequireCommit(ctx, repo, o); err != nil {
			return false, err
		}
	}
	_, err := g.run(ctx, repo, nil, "merge-base", "--is-ancestor", string(from), string(to))
	if err == nil {
		return true, nil
	}
	var command *Error
	if errors.As(err, &command) && command.ExitCode == 1 && !errors.Is(err, process.ErrTreeNotStopped) &&
		!errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return false, nil
	}
	return false, err
}
