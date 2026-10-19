package gitops

import (
	"context"
	"errors"
	"strings"
)

// QueryOptional distinguishes an absent ref from a failed remote observation.
func (g *Client) QueryOptional(ctx context.Context, remote, ref string) (OID, error) {
	if err := g.validRef(ctx, ref); err != nil {
		return "", err
	}
	var dir string
	var err error
	if strings.Contains(remote, "://") {
		dir, err = httpsRemote(remote)
	} else {
		dir, err = g.bareRemote(ctx, remote)
	}
	if err != nil {
		return "", err
	}
	out, err := g.run(ctx, "", g.authEnv(dir), "ls-remote", "--refs", "--", dir, ref)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", nil
	}
	if len(fields) != 2 || fields[1] != ref {
		return "", ErrGit
	}
	return ParseOID(fields[0])
}

// UpdateWorkBranch permits rewriting only the dedicated work namespace, with
// an explicit old-value lease. Protected branch publication still uses Publish.
func (g *Client) UpdateWorkBranch(ctx context.Context, repo, remote, ref string, expected, desired OID) error {
	if !strings.HasPrefix(ref, "refs/heads/coordica/") {
		return ErrInvalidRef
	}
	if err := g.validRef(ctx, ref); err != nil {
		return err
	}
	if err := validIDs(desired); err != nil {
		return err
	}
	if expected != "" {
		if err := validIDs(expected); err != nil {
			return err
		}
	}
	observed, err := g.QueryOptional(ctx, remote, ref)
	if err != nil {
		return err
	}
	if observed == desired {
		return nil
	} // Lost ACK for this exact desired value.
	if observed != expected {
		return ErrCAS
	}
	_, pushErr := g.run(ctx, repo, g.authEnv(remote), "push", "--porcelain", "--no-verify", "--force-with-lease="+ref+":"+string(expected), "--", remote, string(desired)+":"+ref)
	observed, err = g.QueryOptional(ctx, remote, ref)
	if err != nil || ctx.Err() != nil {
		return errors.Join(ErrUnknown, pushErr, err, ctx.Err())
	}
	if observed == desired {
		return nil
	}
	if observed != expected {
		return errors.Join(ErrCAS, pushErr)
	}
	if pushErr == nil {
		return ErrUnknown
	}
	return pushErr
}
