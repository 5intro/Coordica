package gitops

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestWorkBranchCASCreateRewriteAndLostACKReplay(t *testing.T) {
	g := gitClient(t)
	repo, b := newRepo(t, g)
	ctx := context.Background()
	remote := filepath.Join(t.TempDir(), "remote.git")
	if e := g.Init(ctx, remote, true); e != nil {
		t.Fatal(e)
	}
	ref := "refs/heads/coordica/task_example"
	if e := g.UpdateWorkBranch(ctx, repo, remote, ref, "", b); e != nil {
		t.Fatal(e)
	}
	if e := g.UpdateWorkBranch(ctx, repo, remote, ref, "", b); e != nil {
		t.Fatal("lost ACK was not idempotent", e)
	}
	writeFile(t, repo, "src/next.txt", "next\n")
	next := commit(t, g, repo, "next")
	if e := g.UpdateWorkBranch(ctx, repo, remote, ref, b, next); e != nil {
		t.Fatal(e)
	}
	// A repair can replace a candidate without making the previous candidate
	// its ancestor, while protected-branch publication retains FF-only semantics.
	if e := g.UpdateWorkBranch(ctx, repo, remote, ref, next, b); e != nil {
		t.Fatal("work branch rewrite rejected", e)
	}
	if e := g.UpdateWorkBranch(ctx, repo, remote, ref, next, next); !errors.Is(e, ErrCAS) {
		t.Fatal("stale writer overwrote branch", e)
	}
	if e := g.UpdateWorkBranch(ctx, repo, remote, "refs/heads/main", "", next); !errors.Is(e, ErrInvalidRef) {
		t.Fatal("protected target accepted by work-branch API", e)
	}
	got, e := g.Query(ctx, remote, ref)
	if e != nil || got != b {
		t.Fatal("wrong work ref", got, e)
	}
}
