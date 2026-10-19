package centerhost

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGitHubHostCredentialsRemainFixedAndOutsideSource(t *testing.T) {
	c, path, _, _ := hostFixture(t)
	c.TargetRepository = "https://github.com/example/repo.git"
	c.GitHubRepository = "example/repo"
	c.GitHubTokenFile = filepath.Join(c.DataDir, "github-api.token")
	c.GitAuthFile = filepath.Join(c.DataDir, "github-git.header")
	c.GitAuthRemote = c.TargetRepository
	if e := Validate(c, path); e != nil {
		t.Fatal(e)
	}
	equivalent := c
	equivalent.GitHubRepository = "EXAMPLE/RePo"
	if e := Validate(equivalent, path); e != nil {
		t.Fatal("equivalent GitHub repository casing rejected", e)
	}
	for _, change := range []func(*Config){
		func(c *Config) { c.GitHubRepository = "other/repo" },
		func(c *Config) { c.GitHubTokenFile = "relative-token" },
		func(c *Config) { c.GitHubTokenFile = c.ChatProfile },
		func(c *Config) { c.GitAuthRemote = "https://github.com/other/repo.git" },
		func(c *Config) { c.GitHubTokenFile = filepath.Join(c.Repository, "tracked-token") },
	} {
		bad := c
		change(&bad)
		if e := Validate(bad, path); e == nil {
			t.Fatal("unsafe credential configuration accepted")
		}
	}
}

func TestGitHubCredentialRejectsProtectedAssetBeforeCreation(t *testing.T) {
	c, path, _, _ := hostFixture(t)
	c.TargetRepository, c.GitHubRepository = "https://github.com/example/repo.git", "example/repo"
	c.GitHubTokenFile = filepath.Join(c.DataDir, "owner.token")
	if e := Validate(c, path); e == nil {
		t.Fatal("not-yet-created Owner credential accepted as GitHub token")
	}
	if _, e := os.Stat(c.GitHubTokenFile); !os.IsNotExist(e) {
		t.Fatal("validation created credential", e)
	}
}
