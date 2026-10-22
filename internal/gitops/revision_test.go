package gitops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequireCommitRejectsSHA1LengthPrefixesInSHA256Repositories(t *testing.T) {
	g := New("")
	ctx := context.Background()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := g.InitObjectFormat(ctx, repo, false, OID(strings.Repeat("a", 64))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "file"), []byte("fixed object\n"), 0600); err != nil {
		t.Fatal(err)
	}
	commit, err := g.CommitAll(ctx, repo, "fixed", Identity{})
	if err != nil || len(commit) != 64 {
		t.Fatal("SHA-256 fixture failed", commit, err)
	}
	if err := g.RequireCommit(ctx, repo, commit); err != nil {
		t.Fatal("full available commit rejected", err)
	}
	if err := g.RequireCommit(ctx, repo, commit[:40]); err == nil {
		t.Fatal("40-character prefix was accepted as a full SHA-256 commit")
	}
}
