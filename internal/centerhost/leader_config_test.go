package centerhost

import (
	"context"
	"path/filepath"
	"testing"
)

func TestCenterHostCanInitializeWithoutCodexAndProtectsLeaderPaths(t *testing.T) {
	c, path, manager, installer := hostFixture(t)
	path = filepath.Join(filepath.Dir(path), "dsh-center.json")
	c.CodexExecutable = ""
	c.LeaderExecutables = map[string]string{"dsh": filepath.Join(filepath.Dir(c.Executable), "dsh")}
	if err := Save(path, c); err != nil {
		t.Fatal("Codex became a mandatory installation", err)
	}
	loaded, err := Load(path)
	if err != nil || loaded.CodexExecutable != "" || loaded.LeaderExecutables["dsh"] != c.LeaderExecutables["dsh"] {
		t.Fatal(loaded, err)
	}
	manager.ConfigPath = path
	status, err := manager.Initialize(context.Background())
	if err != nil || installer.Calls != 1 || status.ProjectPaused == nil || !*status.ProjectPaused {
		t.Fatal("center orchestration required a Codex installation or resumed work", status, err)
	}
	for _, invalid := range []string{"relative/dsh", filepath.Join(c.DataDir, "owner.token"), path, c.ChatProfile} {
		c.LeaderExecutables["dsh"] = invalid
		if err := Validate(c, path); err == nil {
			t.Fatal("Leader path aliased protected asset", invalid)
		}
	}
	c.LeaderExecutables = nil
	c.LeaderToolsFile = filepath.Join(c.DataDir, "coordica.db")
	if err := Validate(c, path); err == nil {
		t.Fatal("tool registrations aliased database")
	}
}
