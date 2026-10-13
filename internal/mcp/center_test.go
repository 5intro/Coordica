package mcp

import (
	"bytes"
	"context"
	"coordica/internal/centerhost"
	"coordica/internal/domain"
	"coordica/internal/nodeservice"
	"coordica/internal/onboarding"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCenterOfflineRegistryAndForgedFixedHostArguments(t *testing.T) {
	c := CenterLocal{}
	tools, e := c.Tools(context.Background())
	if e != nil || len(tools) != 3 || hasTool(tools, "initialize_runner") {
		t.Fatal("center offline registry incorrect")
	}
	for _, name := range []string{"initialize_center", "start_center", "get_host_status"} {
		for _, args := range []string{`{"host_role":"center"}`, `{"executable":"/arbitrary"}`, `{"data":"/private"}`, `{"url":"http://evil"}`, `{"pid":123}`} {
			if _, bad := c.Call(context.Background(), name, json.RawMessage(args)); !bad {
				t.Fatal("forged fixed host argument accepted", name, args)
			}
		}
	}
	var out bytes.Buffer
	input := bootstrapInitialize + "\n" + `{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n" + `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"initialize_runner","arguments":{}}}`
	if e := (Interaction{Center: &c}).Serve(context.Background(), strings.NewReader(input), &out); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(out.String(), "initialize_center") || strings.Contains(out.String(), "owner.token") || !strings.Contains(out.String(), "center_url") || !strings.Contains(out.String(), "save_runner_profile") {
		t.Fatal("offline center registry leaked authority", out.String())
	}
}
func TestNodeAndRunCannotForgeCenterLifecycleOrBaselineApproval(t *testing.T) {
	for _, name := range []string{"initialize_center", "start_center", "get_host_status", "confirm_project_initialization", "approve_baseline"} {
		var out bytes.Buffer
		input := bootstrapInitialize + "\n" + `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"` + name + `","arguments":{}}}`
		if e := (Interaction{}).Serve(context.Background(), strings.NewReader(input), &out); e != nil {
			t.Fatal(e)
		}
		if !strings.Contains(out.String(), "Unknown tool") {
			t.Fatal("node reached center lifecycle", name, out.String())
		}
		out.Reset()
		if e := (Server{RunID: "assigned", Generation: 1}).Serve(context.Background(), strings.NewReader(input), &out); e != nil {
			t.Fatal(e)
		}
		if !strings.Contains(out.String(), "Unknown tool") {
			t.Fatal("Run reached center lifecycle", name, out.String())
		}
	}
}

func TestCenterAndRunnerShareOneStdioProviderWithOneInspection(t *testing.T) {
	bootstrapFixtures(t, "git")
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "coordica")
	if err = os.WriteFile(binary, []byte("fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	leader := filepath.Join(dir, "codex")
	if err = os.WriteFile(leader, []byte("fixture leader"), 0700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "host.json")
	if err = centerhost.Save(config, centerhost.Config{Version: 1, Role: "center", Executable: binary, CodexExecutable: leader, DataDir: filepath.Join(dir, "center"), ChatProfile: filepath.Join(dir, "chat.json"), Repository: filepath.Join(dir, "source"), Branch: "main", Name: "Fixture", ConsoleListen: "127.0.0.1:18779", RunnerListen: "127.0.0.1:18779", RuntimePath: "/usr/bin:/bin", Checks: []domain.Check{}}); err != nil {
		t.Fatal(err)
	}
	manager := centerhost.Manager{ConfigPath: config, Installer: centerStdioInstaller{}, ReservePorts: func(context.Context, []string) (func(), error) { return func() {}, nil }}
	runnerDir := filepath.Join(dir, "runner")
	calls, provisions := 0, 0
	b := Bootstrap{DataDir: runnerDir, initialize: func(_ context.Context, c onboarding.Config) (onboarding.Result, error) {
		calls++
		if c.DataDir != runnerDir {
			t.Fatal("Runner path not startup fixed", c)
		}
		return onboarding.Result{Registered: true, NodeID: "node_saved"}, nil
	}, ProvisionChat: func(context.Context) error { provisions++; return nil }}
	input := strings.Join([]string{bootstrapInitialize, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"initialize_center","arguments":{}}}`, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"initialize_runner","arguments":{"center_url":"http://127.0.0.1:8080","pairing_code":"fixture"}}}`}, "\n")
	var out bytes.Buffer
	if e := (Interaction{Center: &CenterLocal{Manager: manager}, Bootstrap: b}).Serve(context.Background(), strings.NewReader(input), &out); e != nil {
		t.Fatal(e)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 || strings.Count(lines[1], `"name":"inspect_environment"`) != 1 || !strings.Contains(lines[1], "initialize_center") || !strings.Contains(lines[1], "initialize_runner") || calls != 1 || provisions != 1 {
		t.Fatal("center dropped Runner or duplicated inspection", out.String(), calls, provisions)
	}
	if strings.Contains(lines[2], `"isError":true`) || strings.Contains(lines[3], `"isError":true`) {
		t.Fatal("Runner inaccessible after center operation", out.String())
	}
}
func TestCenterFixedRunnerAssetsRejectDataAndProfileAliases(t *testing.T) {
	root := t.TempDir()
	runner := filepath.Join(root, "runner")
	center := filepath.Join(root, "center")
	chat := filepath.Join(root, "chat.json")
	config := filepath.Join(root, "host.json")
	if e := ValidateCenterPaths(filepath.Join(runner, "discovery.json"), runner, chat, center, config); e != nil {
		t.Fatal(e)
	}
	for _, r := range []string{center, filepath.Join(center, "runner"), root} {
		if e := ValidateCenterPaths(filepath.Join(r, "discovery.json"), r, chat, center, config); e == nil {
			t.Fatal("overlapping role dirs accepted", r)
		}
	}
	binary := filepath.Join(root, "coordica")
	if e := ValidateCenterPaths(binary, runner, chat, center, config, binary); e == nil {
		t.Fatal("discovery overwrites fixed executable")
	}
	if e := ValidateCenterPaths(config, runner, chat, center, config); e == nil {
		t.Fatal("discovery overwrites host config")
	}
}

type centerStdioInstaller struct{}

func (centerStdioInstaller) EnsureStarted(context.Context, nodeservice.Spec) (nodeservice.Status, error) {
	return nodeservice.Status{State: "activation_requested"}, nil
}
