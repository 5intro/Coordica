package gitops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalysisPinnedPublicTrackedSourceAndUnbornDetection(t *testing.T) {
	ctx := context.Background()
	g := New("git")
	dir := t.TempDir()
	repo := filepath.Join(dir, "source")
	if _, e := g.AnalysisRevision(ctx, repo, "main"); e != ErrUnborn {
		t.Fatal("new fixed directory not detected", e)
	}
	if e := g.Init(ctx, repo, false); e != nil {
		t.Fatal(e)
	}
	if _, e := g.AnalysisRevision(ctx, repo, "main"); e != ErrUnborn {
		t.Fatal("unborn repository not detected", e)
	}
	for path, text := range map[string]string{"README.md": "public", "src/main.go": "package main", "internal/chat_credentials.go": "package internal", ".env": "private sentinel", "docs/private.md": "private handoff sentinel", ".gitignore": "ignored.txt\n", "ignored.txt": "ignored sentinel"} {
		if e := os.MkdirAll(filepath.Dir(filepath.Join(repo, path)), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(filepath.Join(repo, path), []byte(text), 0600); e != nil {
			t.Fatal(e)
		}
	}
	sha, e := g.CommitAll(ctx, repo, "initial", Identity{})
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(repo, "README.md"), []byte("changed working tree"), 0600)
	snapshot, e := g.ReadAnalysis(ctx, repo, string(sha))
	if e != nil {
		t.Fatal(e)
	}
	seen := map[string]string{}
	for _, file := range snapshot.Files {
		seen[file.Path] = file.Text
		if strings.Contains(file.Text, "sentinel") {
			t.Fatal("private source leaked")
		}
	}
	if seen["README.md"] != "public" || seen["internal/chat_credentials.go"] == "" || seen["ignored.txt"] != "" || seen["docs/private.md"] != "" || snapshot.SHA != string(sha) {
		t.Fatal("snapshot not fixed public source", seen)
	}
}
func TestAnalysisRelevantOversizeCannotBecomeReady(t *testing.T) {
	ctx := context.Background()
	g := New("git")
	repo := filepath.Join(t.TempDir(), "repo")
	if e := g.Init(ctx, repo, false); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(repo, "large.go"), []byte(strings.Repeat("x", (512<<10)+1)), 0600)
	sha, e := g.CommitAll(ctx, repo, "oversize", Identity{})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.ReadAnalysis(ctx, repo, string(sha)); e == nil {
		t.Fatal("relevant source silently truncated")
	}
}
