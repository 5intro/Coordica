package centerhost

import (
	"path/filepath"
	"testing"
)

func TestTrustedCheckMetadataRoundTripsWithoutConfigurationConflicts(t *testing.T) {
	c, path, _, _ := hostFixture(t)
	c.Checks[0].RequiredPaths, c.Checks[0].OutputPaths = []string{}, []string{}
	if err := Save(path, c); err != nil {
		t.Fatal("explicit empty declarations changed the saved identity", err)
	}
	c.Checks[0].RequiredPaths = []string{"src/main.go"}
	c.Checks[0].OutputPaths = []string{"build/**"}
	path = filepath.Join(filepath.Dir(path), "declared-checks.json")
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil || loaded.Checks[0].RequiredPaths[0] != "src/main.go" || loaded.Checks[0].OutputPaths[0] != "build/**" {
		t.Fatal("trusted metadata was dropped", loaded, err)
	}
	if err := Save(path, c); err != nil {
		t.Fatal("identical metadata could not be retried", err)
	}
	c.Checks[0].OutputPaths = []string{"other/**"}
	if err := Save(path, c); err == nil {
		t.Fatal("saved host identity silently changed check output authority")
	}
}
