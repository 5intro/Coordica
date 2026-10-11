package main

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"testing"

	"coordica/internal/mcp"
)

func TestBootstrapProfileIsLocalConfigurationOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Agent Tools", "工具")
	profile := mcp.BootstrapProfile{Name: "laptop", Platform: runtime.GOOS + "/" + runtime.GOARCH, Executables: map[string]string{"codex": path}}
	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeBootstrapProfile(raw)
	if err != nil || got.Executables["codex"] != path {
		t.Fatalf("profile = %+v, error = %v", got, err)
	}
	for _, raw := range []string{
		`null`,
		`{"platform":"another/platform"}`,
		`{"platform":"` + profile.Platform + `","token":"do-not-import"}`,
		`{"platform":"` + profile.Platform + `","executables":{"shell":""}}`,
		`{"platform":"` + profile.Platform + `","executables":{"codex":"relative/tool"}}`,
		`{"platform":"` + profile.Platform + `"} {}`,
	} {
		if _, err := decodeBootstrapProfile([]byte(raw)); err == nil {
			t.Fatalf("accepted invalid profile %s", raw)
		}
	}
}
