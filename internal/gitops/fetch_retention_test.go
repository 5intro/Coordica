package gitops

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestFetchRetainsDistinctObservedObjectsWithoutPollingRefLeak(t *testing.T) {
	g := gitClient(t)
	source, base := newRepo(t, g)
	ctx := context.Background()
	remote := filepath.Join(t.TempDir(), "remote.git")
	if err := g.Init(ctx, remote, true); err != nil {
		t.Fatal(err)
	}
	mustRun(t, g, source, "push", "--", remote, string(base)+":refs/heads/main")
	cache := filepath.Join(t.TempDir(), "cache")
	if err := g.Clone(ctx, remote, cache); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if got, err := g.Fetch(ctx, cache, remote, "refs/heads/main"); err != nil || got != base {
			t.Fatal(got, err)
		}
	}
	refs := string(mustRun(t, g, cache, "for-each-ref", "--format=%(refname)", "refs/coordica"))
	if strings.Contains(refs, "/fetch/") || strings.Count(refs, "/observed/") != 1 {
		t.Fatal("polling leaked transient refs", refs)
	}
	writeFile(t, source, "src/new.txt", "new main\n")
	next := commit(t, g, source, "next")
	mustRun(t, g, source, "push", "--", remote, string(next)+":refs/heads/main")
	var wg sync.WaitGroup
	failures := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := g.Fetch(ctx, cache, remote, "refs/heads/main")
			if err != nil {
				failures <- err
			} else if got != next {
				failures <- fmt.Errorf("stale observed object %s", got)
			}
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	refs = string(mustRun(t, g, cache, "for-each-ref", "--format=%(refname)", "refs/coordica"))
	if strings.Contains(refs, "/fetch/") || strings.Count(refs, "/observed/") != 2 {
		t.Fatal("distinct observations or concurrency lost", refs)
	}
	mustRun(t, g, cache, "gc", "--prune=now")
	for _, oid := range []OID{base, next} {
		if err := g.requireCommit(ctx, cache, oid); err != nil {
			t.Fatal("observation lost after pruning", err)
		}
	}
	// The named cached main branch belongs to clone, never to Fetch.
	if got := strings.TrimSpace(string(mustRun(t, g, cache, "rev-parse", "refs/heads/main"))); got != string(base) {
		t.Fatal("Fetch overwrote cached branch", got)
	}
}

func TestCloneLegacyObservationRefsDoesNotOverflowProgressOutput(t *testing.T) {
	g := gitClient(t)
	source, base := newRepo(t, g)
	var input strings.Builder
	for i := 0; i < 1200; i++ {
		fmt.Fprintf(&input, "create refs/coordica/fetch/legacy-%08d-%s %s\n", i, strings.Repeat("x", 48), base)
	}
	cmd := exec.Command(g.GitPath, "-C", source, "update-ref", "--stdin")
	cmd.Env = controlledEnv(nil)
	cmd.Stdin = strings.NewReader(input.String())
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(err, string(output))
	}
	clone := filepath.Join(t.TempDir(), "clone")
	if err := g.Clone(context.Background(), source, clone); err != nil {
		t.Fatal("legacy ref announcements exceeded diagnostic bound", err)
	}
	refs := strings.Fields(string(mustRun(t, g, clone, "for-each-ref", "--format=%(refname)", "refs/coordica/fetch")))
	if len(refs) != 1200 {
		t.Fatal("quiet transfer dropped retained references", len(refs))
	}
	if err := g.Checkout(context.Background(), clone, base); err != nil {
		t.Fatal(err)
	}
}

func TestFetchNeverOverwritesMismatchedObservationPin(t *testing.T) {
	g := gitClient(t)
	source, base := newRepo(t, g)
	remote := filepath.Join(t.TempDir(), "remote.git")
	ctx := context.Background()
	if err := g.Init(ctx, remote, true); err != nil {
		t.Fatal(err)
	}
	writeFile(t, source, "src/new.txt", "next\n")
	next := commit(t, g, source, "next")
	mustRun(t, g, source, "push", "--", remote, string(next)+":refs/heads/main")
	cache := filepath.Join(t.TempDir(), "cache")
	if err := g.Clone(ctx, remote, cache); err != nil {
		t.Fatal(err)
	}
	pin := "refs/coordica/observed/" + string(next)
	mustRun(t, g, cache, "update-ref", pin, string(base))
	if _, err := g.Fetch(ctx, cache, remote, "refs/heads/main"); err == nil {
		t.Fatal("mismatched immutable pin accepted")
	}
	if got := strings.TrimSpace(string(mustRun(t, g, cache, "rev-parse", pin))); got != string(base) {
		t.Fatal("immutable reference overwritten")
	}
}
