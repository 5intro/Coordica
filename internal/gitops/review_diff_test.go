package gitops

import (
	"context"
	"strings"
	"testing"
)

func TestDiffTextPreviewAccountsForBinaryNamesAndAttributes(t *testing.T) {
	g := gitClient(t)
	repo, base := newRepo(t, g)
	writeFile(t, repo, "src/tab\t 大图.png", "\x00binary image fixture")
	writeFile(t, repo, "src/renamed.png", "\x00another binary")
	writeFile(t, repo, "src/hidden.js", "code marked binary by attributes\n")
	writeFile(t, repo, ".gitattributes", "*.js -diff\n")
	writeFile(t, repo, "src/main.txt", "actual source change\n")
	head := commit(t, g, repo, "fixed inputs")
	preview, binaries, err := g.DiffTextPreview(context.Background(), repo, base, head)
	if err != nil || len(binaries) != 3 || !strings.Contains(strings.Join(binaries, "|"), "src/tab\t 大图.png") ||
		!strings.Contains(strings.Join(binaries, "|"), "src/hidden.js") || !strings.Contains(string(preview), "+actual source change") ||
		strings.Contains(string(preview), "GIT binary patch") {
		t.Fatal("lost text or binary path accounting", binaries, err)
	}
	for _, oid := range []OID{"HEAD", "-option"} {
		if _, _, err = g.DiffTextPreview(context.Background(), repo, oid, head); err == nil {
			t.Fatal("unpinned preview accepted")
		}
		if _, err = g.ReadBlob(context.Background(), repo, oid); err == nil {
			t.Fatal("unpinned blob read accepted")
		}
	}
}
