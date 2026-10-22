package gitops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func forwardSourceGit() int {
	if rawGitHelperArgument("cat-file") >= 0 {
		f, err := os.OpenFile(os.Getenv("COORDICA_SOURCE_COUNT"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return 93
		}
		_, err = f.WriteString("cat-file\n")
		if closeErr := f.Close(); err != nil || closeErr != nil {
			return 93
		}
	}
	cmd := exec.Command(os.Getenv("COORDICA_SOURCE_GIT"), os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return 92
	}
	return 0
}

func TestSourceReaderBatchesThousandsOfPinnedBlobs(t *testing.T) {
	g := gitClient(t)
	repo, _ := newRepo(t, g)
	const count = 1600
	for i := 0; i < count; i++ {
		writeFile(t, repo, fmt.Sprintf("src/file%04d.js", i), fmt.Sprintf("export const value = %d;\n", i))
	}
	base := commit(t, g, repo, "many fixed source objects")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "commands")
	t.Setenv("COORDICA_GIT_RAW_TEST_MODE", "source-batch-forward")
	t.Setenv("COORDICA_SOURCE_COUNT", marker)
	t.Setenv("COORDICA_SOURCE_GIT", g.GitPath)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	seen := map[string]bool{}
	err = (SourceReader{Git: New(exe)}).Read(ctx, repo, base, func(s string) bool { return strings.HasSuffix(s, ".js") },
		func(name, text string) error {
			seen[name] = true
			if !strings.HasPrefix(text, "export const value = ") {
				return errors.New("immutable source content changed")
			}
			return nil
		})
	if err != nil || len(seen) != count {
		t.Fatal("bounded many-file scan failed", len(seen), err)
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "cat-file\n" {
		t.Fatal("scan launched one cat-file process per blob", string(data), err)
	}
}

func TestSourceBatchRejectsBrokenFramingAndHonorsConsumerFailure(t *testing.T) {
	oid := strings.Repeat("a", 40)
	blobs := []sourceBlob{{"src/a.js", oid, 3}}
	header := oid + " blob 3\n"
	for _, response := range []string{"", oid + " missing\n", header + "ab", header + "abcX", header + "abc\nextra"} {
		if err := consumeSourceBatch(context.Background(), []byte(response), blobs, func(string, string) error { return nil }); err == nil {
			t.Fatal("invalid batch accepted", response)
		}
	}
	want := errors.New("consumer failed")
	err := consumeSourceBatch(context.Background(), []byte(header+"abc\n"), blobs, func(string, string) error { return want })
	if !errors.Is(err, want) {
		t.Fatal("consumer error lost", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := consumeSourceBatch(ctx, []byte(header+"abc\n"), blobs, func(string, string) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled scan proceeded", err)
	}
}
