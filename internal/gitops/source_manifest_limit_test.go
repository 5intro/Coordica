package gitops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func longSourceManifestGit() int {
	if rawGitHelperArgument("ls-tree") >= 0 {
		if os.Getenv("COORDICA_SOURCE_STDERR") == "1" {
			fmt.Fprint(os.Stderr, strings.Repeat("e", 70<<10))
			return 0
		}
		prefix := strings.Repeat("segment/", 200)
		for i := 0; i < 11000; i++ {
			if _, err := fmt.Fprintf(os.Stdout, "100644 blob %s 1\t%sfile%05d.js\x00", os.Getenv("COORDICA_SOURCE_BLOB"), prefix, i); err != nil {
				return 91
			}
		}
		return 0
	}
	cmd := exec.Command(os.Getenv("COORDICA_SOURCE_GIT"), os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return 92
	}
	return 0
}

func TestSourceManifestStderrOverflowIsNotAnInventoryDefect(t *testing.T) {
	g := gitClient(t)
	repo, head := newRepo(t, g)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("COORDICA_GIT_RAW_TEST_MODE", "source-manifest-long")
	t.Setenv("COORDICA_SOURCE_GIT", g.GitPath)
	t.Setenv("COORDICA_SOURCE_STDERR", "1")
	err = (SourceReader{Git: New(exe)}).Read(context.Background(), repo, head, func(string) bool { return true },
		func(string, string) error { t.Fatal("failed scan consumed content"); return nil })
	var defect *SourceInventoryError
	if !errors.Is(err, ErrOutputLimit) || errors.As(err, &defect) {
		t.Fatal("operational stderr failure became candidate source debt", err)
	}
}

func TestSourceManifestSupportsBoundedLongPathInventory(t *testing.T) {
	g := gitClient(t)
	repo, _ := newRepo(t, g)
	writeFile(t, repo, "one.js", "x")
	head := commit(t, g, repo, "one fixed source blob")
	raw, err := g.run(context.Background(), repo, nil, "rev-parse", string(head)+":one.js")
	if err != nil {
		t.Fatal(err)
	}
	oid := strings.TrimSpace(string(raw))
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("COORDICA_GIT_RAW_TEST_MODE", "source-manifest-long")
	t.Setenv("COORDICA_SOURCE_GIT", g.GitPath)
	t.Setenv("COORDICA_SOURCE_BLOB", oid)
	count := 0
	err = (SourceReader{Git: New(exe)}).Read(context.Background(), repo, head, func(string) bool { return true },
		func(name, text string) error {
			if len(name) < 1600 || text != "x" {
				t.Fatal("long manifest changed pinned bytes", name, text)
			}
			count++
			return nil
		})
	if err != nil || count != 11000 {
		t.Fatal("accepted inventory exceeded the generic command cap", count, err)
	}
}
