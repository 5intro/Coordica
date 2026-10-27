package gitops

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestDiffTextPreviewAccountsForBinaryNamesAndAttributes(t *testing.T) {
	g := gitClient(t)
	repo, base := newRepo(t, g)
	writeFile(t, repo, "src/space 大图.png", "\x00binary image fixture")
	writeFile(t, repo, "src/renamed.png", "\x00another binary")
	writeFile(t, repo, "src/hidden.js", "code marked binary by attributes\n")
	writeFile(t, repo, ".gitattributes", "*.js -diff\n")
	writeFile(t, repo, "src/main.txt", "actual source change\n")
	head := commit(t, g, repo, "fixed inputs")
	preview, binaries, err := g.DiffTextPreview(context.Background(), repo, base, head)
	if err != nil || len(binaries) != 3 || !strings.Contains(strings.Join(binaries, "|"), "src/space 大图.png") ||
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

func TestBinaryNumstatPreservesTabsAndRejectsMalformedPaths(t *testing.T) {
	path := "src/tab\t 大图.png"
	got, err := binaryPathsInNumstat([]byte("1\t2\tsrc/code.js\x00-\t-\t" + path + "\x00"))
	if err != nil || !reflect.DeepEqual(got, []string{path}) {
		t.Fatal("NUL-delimited Git paths lost embedded tabs or Unicode", got, err)
	}
	for _, invalid := range []string{"-\t3\tsrc/a\x00", "1\tsrc/a\x00", "-\t-\t../outside\x00", "-\t-\t\xff\x00", "-\t-\t\x00"} {
		if _, err := binaryPathsInNumstat([]byte(invalid)); !errors.Is(err, ErrGit) {
			t.Fatalf("accepted malformed numstat %q: %v", invalid, err)
		}
	}
}
