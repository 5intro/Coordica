package gitops

import (
	"context"
	"strings"
	"testing"
)

func TestSourceReaderPinsObjectsAndDoesNotTrustWorktree(t *testing.T) {
	g := gitClient(t)
	repo, _ := newRepo(t, g)
	writeFile(t, repo, "src/main.go", "package original\n")
	base := commit(t, g, repo, "fixed input")
	writeFile(t, repo, "src/main.go", "package changed\n")
	writeFile(t, repo, "src/new.go", "package newfile\n")
	reader := SourceReader{Git: g}
	include := func(name string) bool { return strings.HasSuffix(name, ".go") }
	seen := map[string]string{}
	consume := func(name, text string) error { seen[name] = text; return nil }
	if err := reader.Read(context.Background(), repo, base, include, consume); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen["src/main.go"] != "package original\n" {
		t.Fatal("mutable worktree entered immutable inventory", seen)
	}
	seen = map[string]string{}
	if err := reader.Worktree(context.Background(), repo, include, consume); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen["src/main.go"] != "package changed\n" {
		t.Fatal("local source inventory missing changes", seen)
	}
}
