package gitops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCheckInputPresenceUsesFixedTreeIncludingOmittedManifests(t *testing.T) {
	g := gitClient(t)
	repo, _ := newRepo(t, g)
	writeFile(t, repo, "package-lock.json", "private/generated contents must not be read")
	commit, err := g.CommitAll(context.Background(), repo, "input manifest", Identity{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo, "package-lock.json")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, repo, "untracked.json", "not fixed")
	inputs, err := g.CheckInputPaths(context.Background(), repo, string(commit), []string{"package-lock.json", "src", "untracked.json", "missing"})
	if err != nil || !reflect.DeepEqual(inputs, []string{"package-lock.json", "src"}) {
		t.Fatal("working files replaced immutable input presence", inputs, err)
	}
}

func TestCheckOutputDiagnosticDistinguishesSourceMutation(t *testing.T) {
	g := gitClient(t)
	repo, commit := newRepo(t, g)
	writeFile(t, repo, "cache/generated", "output")
	err := g.VerifyCheckOutputs(context.Background(), repo, commit, nil)
	var outputs *UndeclaredCheckOutputs
	if !errors.As(err, &outputs) || !errors.Is(err, ErrDirty) || outputs.Count == 0 || len(outputs.Paths) == 0 {
		t.Fatal("undeclared outputs lost typed paths", err)
	}
	if err := g.VerifyCheckOutputs(context.Background(), repo, commit, []string{"cache/**"}); err != nil {
		t.Fatal("declared output rejected", err)
	}
	writeFile(t, repo, "src/main.txt", "source mutated by check")
	err = g.VerifyCheckOutputs(context.Background(), repo, commit, []string{"**"})
	if !errors.Is(err, ErrDirty) || errors.As(err, &outputs) {
		t.Fatal("source mutation mistaken for disposable output", err)
	}
}
