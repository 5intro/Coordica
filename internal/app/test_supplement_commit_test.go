package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestSupplementRejectsUnavailableAndNonCommitObjects(t *testing.T) {
	for _, kind := range []string{"missing-sha1", "missing-sha256", "tree", "blob", "tag"} {
		t.Run(kind, func(t *testing.T) {
			f := newITFixture(t)
			c, plan, _ := catalogDefinition(t, f)
			if _, err := f.w.Base(context.Background()); err != nil {
				t.Fatal(err)
			}
			repo := filepath.Join(f.s.Dir, "repository")
			commit := strings.Repeat("c", 40)
			switch kind {
			case "missing-sha256":
				commit = strings.Repeat("c", 64)
			case "tree":
				commit = itGit(t, f.g.GitPath, repo, "rev-parse", f.b.String()+"^{tree}")
			case "blob":
				commit = itGit(t, f.g.GitPath, repo, "rev-parse", f.b.String()+":src/base.txt")
			case "tag":
				itGit(t, f.g.GitPath, repo, "-c", "user.name=Coordica", "-c", "user.email=coordica@example.invalid",
					"tag", "-a", "test-object", f.b.String(), "-m", "annotated tag")
				commit = itGit(t, f.g.GitPath, repo, "rev-parse", "refs/tags/test-object")
			}
			coverage, err := c.Coverage(context.Background(), f.owner, plan.RequirementID, commit)
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Supplement(context.Background(), f.owner, "unavailable", domain.TestSupplementRequest{
				RequirementID: plan.RequirementID, Commit: commit, ExpectedDigest: coverage.Digest})
			if err == nil {
				t.Fatal("unavailable/non-commit object permanently poisoned backup closure", kind)
			}
			if err := f.s.DB.View(context.Background(), func(tx *store.Tx) error {
				plans, err := store.List[domain.TestSupplementPlan](tx, "test_supplement")
				if err == nil && len(plans) != 0 {
					t.Fatal("rejected request left durable supplemental work", plans)
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := f.w.Backup(context.Background(), f.owner); err != nil {
				t.Fatal("rejected request damaged backup availability", err)
			}
		})
	}
}

func TestSupplementReplayDoesNotRequireGitToRemainOnline(t *testing.T) {
	f := newITFixture(t)
	c, plan, _ := catalogDefinition(t, f)
	if _, err := f.w.Base(context.Background()); err != nil {
		t.Fatal(err)
	}
	coverage := catalogCoverage(t, c, f, plan)
	in := domain.TestSupplementRequest{RequirementID: plan.RequirementID, Commit: f.b.String(), ExpectedDigest: coverage.Digest}
	first, err := c.Supplement(context.Background(), f.owner, "accepted", in)
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(f.s.Dir, "repository")
	if err := os.Rename(repo, repo+"-offline"); err != nil {
		t.Fatal(err)
	}
	replayed, err := c.Supplement(context.Background(), f.owner, "accepted", in)
	if err != nil || string(first) != string(replayed) {
		t.Fatal("accepted receipt replay depends on later Git availability", err)
	}
	if _, err := c.Supplement(context.Background(), f.owner, "fresh-key", in); err == nil {
		t.Fatal("fresh admission skipped commit availability")
	}
}
