package gitops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorktreeReadsSparseIndexButPreservesRealDeletionsAndLocalEdits(t *testing.T) {
	g := gitClient(t)
	repo, _ := newRepo(t, g)
	ctx := context.Background()
	writeFile(t, repo, "kept/local.js", "committed\n")
	writeFile(t, repo, "kept/deleted.js", "deleted locally\n")
	text := "function work() {\n" + strings.Repeat("work();\n", 81) + "}\n"
	writeFile(t, repo, "hidden/feature.js", text)
	commit(t, g, repo, "commit branch work before sparse checkout")
	for _, args := range [][]string{{"sparse-checkout", "init", "--cone"}, {"sparse-checkout", "set", "kept"}} {
		if out, err := g.run(ctx, repo, nil, args...); err != nil {
			t.Fatal(string(out), err)
		}
	}
	if _, err := os.Stat(filepath.Join(repo, "hidden", "feature.js")); !os.IsNotExist(err) {
		t.Fatal("fixture is not sparse", err)
	}
	writeFile(t, repo, "kept/local.js", "local edit\n")
	writeFile(t, repo, "kept/new.js", "new untracked source\n")
	if err := os.Remove(filepath.Join(repo, "kept", "deleted.js")); err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	err := (SourceReader{Git: g}).Worktree(ctx, repo, func(name string) bool {
		return strings.HasSuffix(name, ".js")
	}, func(name, text string) error { seen[name] = text; return nil })
	if err != nil || seen["hidden/feature.js"] != text || seen["kept/local.js"] != "local edit\n" {
		t.Fatal("sparse or modified source was omitted", seen, err)
	}
	if _, exists := seen["kept/deleted.js"]; exists || seen["kept/new.js"] != "new untracked source\n" || len(seen) != 3 {
		t.Fatal("local deletion or untracked source was misclassified", seen)
	}
}
