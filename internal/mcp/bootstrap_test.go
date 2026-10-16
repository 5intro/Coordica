package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"coordica/internal/agent"
	"coordica/internal/nodeservice"
	"coordica/internal/onboarding"
	"coordica/internal/runner"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
		if log := os.Getenv("MCP_FIXTURE_ARGV_LOG"); log != "" {
			file, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
			if err != nil {
				os.Exit(2)
			}
			fmt.Fprintf(file, "%s --version\n", name)
			file.Close()
		}
		if os.Getenv("MCP_FIXTURE_FAIL") == name {
			os.Exit(1)
		}
		fmt.Printf("%s fixture 1.0 %s\n", name, os.Getenv("MCP_FIXTURE_API_KEY"))
		os.Exit(0)
	}
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-test.") {
		if log := os.Getenv("MCP_FIXTURE_ARGV_LOG"); log != "" {
			file, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
			if err == nil {
				fmt.Fprintf(file, "unexpected fixture command: %s\n", os.Args[1])
				file.Close()
			}
		}
		os.Exit(2)
	}
	os.Exit(m.Run())
}

func bootstrapFixtures(t *testing.T, names ...string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "tools with spaces 本机")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		if err = os.WriteFile(filepath.Join(dir, name), data, 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	return dir
}

const bootstrapInitialize = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"fixture","version":"1"}}}`

type bootstrapReply struct {
	ID     json.RawMessage `json:"id"`
	Error  json.RawMessage `json:"error"`
	Result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	} `json:"result"`
}

func bootstrapReplies(t *testing.T, b Bootstrap, lines ...string) []bootstrapReply {
	t.Helper()
	var out bytes.Buffer
	if err := b.Serve(context.Background(), strings.NewReader(strings.Join(lines, "\n")), &out); err != nil {
		t.Fatal(err)
	}
	var replies []bootstrapReply
	decoder := json.NewDecoder(&out)
	for {
		var reply bootstrapReply
		if err := decoder.Decode(&reply); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		replies = append(replies, reply)
	}
	return replies
}

func TestBootstrapAcceptsClientImplementationMetadata(t *testing.T) {
	replies := bootstrapReplies(t, Bootstrap{},
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"dsh","version":"fixture","title":"DSH","description":"MCP client","websiteUrl":"https://example.test","icons":[{"src":"https://example.test/icon.svg","mimeType":"image/svg+xml"}]}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{"_meta":{"progressToken":1}}}`)
	if len(replies) != 2 || len(replies[0].Error) != 0 || len(replies[1].Result.Tools) != 3 {
		t.Fatalf("legal client metadata rejected: %+v", replies)
	}
	for _, info := range []string{`{"name":1,"version":"1"}`, `{"name":"client","version":null}`} {
		line := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","clientInfo":%s}}`, info)
		if replies = bootstrapReplies(t, Bootstrap{}, line); len(replies[0].Error) == 0 {
			t.Fatalf("invalid client metadata accepted: %s", info)
		}
	}
}

func TestBootstrapInspectsOnlyVersionsAndSavesPrivateProfile(t *testing.T) {
	tools := bootstrapFixtures(t, "git", "codex", "claude", "dsh")
	log := filepath.Join(t.TempDir(), "argv.log")
	t.Setenv("MCP_FIXTURE_ARGV_LOG", log)
	t.Setenv("MCP_FIXTURE_API_KEY", "fixture-sensitive-value")
	profilePath := filepath.Join(t.TempDir(), "private", "profile.json")
	replies := bootstrapReplies(t, Bootstrap{ProfilePath: profilePath}, bootstrapInitialize,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"inspect_environment","arguments":{},"_meta":{"progressToken":"probe"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"save_runner_profile","arguments":{}}}`)
	if len(replies) != 4 || len(replies[1].Result.Tools) != 3 {
		t.Fatalf("unexpected replies: %+v", replies)
	}
	for i, name := range []string{"initialize_runner", "inspect_environment", "save_runner_profile"} {
		if replies[1].Result.Tools[i].Name != name {
			t.Fatalf("unexpected authority: %+v", replies[1])
		}
	}
	var environment bootstrapEnvironment
	if replies[2].Result.IsError || json.Unmarshal([]byte(replies[2].Result.Content[0].Text), &environment) != nil {
		t.Fatalf("inspect failed: %+v", replies[2])
	}
	if environment.OS != runtime.GOOS || environment.Arch != runtime.GOARCH || environment.Host == "" || !environment.Git.Available || environment.Git.Authentication != "unknown" || strings.Contains(environment.Git.Version, "fixture-sensitive-value") {
		t.Fatalf("incorrect environment: %+v", environment)
	}
	for _, name := range []string{"codex", "claude", "dsh"} {
		capability := environment.Tools[name]
		if !capability.Available || capability.Authentication != "unknown" || !strings.HasPrefix(capability.Path, tools) || strings.Contains(capability.Version, "fixture-sensitive-value") {
			t.Fatalf("unsafe capability %s: %+v", name, capability)
		}
	}
	if replies[3].Result.IsError {
		t.Fatal(replies[3])
	}
	data, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	var profile BootstrapProfile
	if json.Unmarshal(data, &profile) != nil || profile.Name != environment.Host || profile.Platform != runtime.GOOS+"/"+runtime.GOARCH || len(profile.Executables) != 3 || profile.Executables["git"] != "" || bytes.Contains(data, []byte("fixture-sensitive-value")) || bytes.Contains(data, []byte("token")) {
		t.Fatalf("unsafe profile: %s", data)
	}
	info, err := os.Stat(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("profile permissions: %v", info.Mode())
	}
	args, err := os.ReadFile(log)
	if err != nil || string(args) != "git --version\ncodex --version\nclaude --version\ndsh --version\n" {
		t.Fatalf("unexpected commands: %q, %v", args, err)
	}
}

func TestBootstrapRejectsArbitraryAuthorityAndInvalidEnvelopes(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "profile.json")
	replies := bootstrapReplies(t, Bootstrap{ProfilePath: profile, DataDir: t.TempDir()}, bootstrapInitialize,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"inspect_environment","arguments":{"executable":"arbitrary"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"save_runner_profile","arguments":{"path":"arbitrary"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"save_runner_profile","arguments":null}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"save_runner_profile","arguments":[]}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"initialize_runner","arguments":{"center_url":"https://center","pairing_code":"code","capacity":null}}}`,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"initialize_runner","arguments":{"center_url":"https://center","pairing_code":"code","data_dir":"arbitrary"}}}`,
		`{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"initialize_runner","arguments":{"center_url":"https://center","pairing_code":"code","capacity":3}}}`,
		`{"jsonrpc":"2.0","id":{},"method":"tools/call","params":{"name":"inspect_environment","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":9.5,"method":"tools/call","params":{"name":"inspect_environment","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":null,"method":"tools/call","params":{"name":"inspect_environment","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"inspect_environment","name":"save_runner_profile","arguments":{}}}`)
	for _, reply := range replies[1:8] {
		if !reply.Result.IsError {
			t.Fatalf("invalid tool input succeeded: %+v", reply)
		}
	}
	for _, reply := range replies[8:] {
		if len(reply.Error) == 0 {
			t.Fatalf("invalid JSON-RPC envelope accepted: %+v", reply)
		}
	}
	if _, err := os.Stat(profile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid input wrote profile: %v", err)
	}
}

func TestBootstrapSaveRequiresFreshInspectionAndFixedDestination(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "profile.json")
	replies := bootstrapReplies(t, Bootstrap{ProfilePath: profile}, bootstrapInitialize,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"save_runner_profile","arguments":{}}}`)
	if !replies[1].Result.IsError {
		t.Fatal("save succeeded without inspection")
	}
	bootstrapFixtures(t, "git")
	for _, path := range []string{"", filepath.Join(t.TempDir(), "runner.json")} {
		replies = bootstrapReplies(t, Bootstrap{ProfilePath: path}, bootstrapInitialize,
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"inspect_environment","arguments":{}}}`,
			`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"save_runner_profile","arguments":{}}}`)
		if !replies[2].Result.IsError {
			t.Fatalf("save allowed invalid destination %q", path)
		}
	}
}

func TestBootstrapInitializeDetectsPathsAndRedactsPairingCode(t *testing.T) {
	bootstrapFixtures(t, "git", "dsh")
	dataDir := filepath.Join(t.TempDir(), "fixed runner directory")
	called := false
	b := Bootstrap{DataDir: dataDir, initialize: func(_ context.Context, config onboarding.Config) (onboarding.Result, error) {
		called = true
		if config.DataDir != dataDir || config.CenterURL != "https://center.example:8443" || config.PairingCode != "fixture-pairing-secret" || config.Capacity != 1 || config.Executables["dsh"] == "" || config.Executables["git"] != "" || config.Name == "" {
			t.Fatalf("wrong handoff: %+v", config)
		}
		return onboarding.Result{NodeID: "fixture-node", Registered: true, Steps: []string{"registration_saved", "service_activation_failed"}}, fmt.Errorf("pairing failed for fixture-pairing-secret")
	}}
	replies := bootstrapReplies(t, b, bootstrapInitialize,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"initialize_runner","arguments":{"center_url":"https://center.example:8443","pairing_code":"fixture-pairing-secret","capacity":1}}}`)
	if !called || !replies[1].Result.IsError || strings.Contains(replies[1].Result.Content[0].Text, "fixture-pairing-secret") || !strings.Contains(replies[1].Result.Content[0].Text, `"registered":true`) {
		t.Fatalf("unsafe initialization result: %+v", replies)
	}
}

func TestBootstrapInitializeDefaultsAndPreservesUseCaseResult(t *testing.T) {
	bootstrapFixtures(t, "git", "dsh")
	dataDir := t.TempDir()
	calls := 0
	b := Bootstrap{DataDir: dataDir, initialize: func(_ context.Context, config onboarding.Config) (onboarding.Result, error) {
		calls++
		if config.Capacity != 2 || config.Name != "Windows-dev" || config.TLSCA != "public-ca.pem" || config.DataDir != dataDir || config.Executables["dsh"] == "" {
			t.Fatalf("unexpected default/handoff: %+v", config)
		}
		return onboarding.Result{NodeID: "fixture-node", Registered: true, Service: nodeservice.Status{Name: "fixture-user-service", State: "activation_requested"}, Steps: []string{"user_service_activated"}}, nil
	}}
	replies := bootstrapReplies(t, b, bootstrapInitialize,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"initialize_runner","arguments":{"center_url":"https://center","pairing_code":"fixture-code","name":"Windows-dev","tls_ca":"public-ca.pem"}}}`)
	if calls != 1 || replies[1].Result.IsError || !strings.Contains(replies[1].Result.Content[0].Text, `"node_id":"fixture-node"`) || !strings.Contains(replies[1].Result.Content[0].Text, `"state":"activation_requested"`) {
		t.Fatalf("wrong activation result: %+v", replies)
	}
}

func TestBootstrapFailedVersionRemainsUnavailableAndAuthenticationUnknown(t *testing.T) {
	bootstrapFixtures(t, "git", "dsh")
	t.Setenv("MCP_FIXTURE_FAIL", "dsh")
	replies := bootstrapReplies(t, Bootstrap{Executables: map[string]string{"codex": filepath.Join(t.TempDir(), "not-installed", "codex")}}, bootstrapInitialize,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"inspect_environment","arguments":{}}}`)
	var environment bootstrapEnvironment
	if replies[1].Result.IsError || json.Unmarshal([]byte(replies[1].Result.Content[0].Text), &environment) != nil {
		t.Fatal(replies[1])
	}
	if environment.Tools["dsh"].Available || environment.Tools["dsh"].Authentication != "unknown" || environment.Tools["dsh"].Detail == "" || environment.Tools["codex"].Available {
		t.Fatalf("failed or missing tools appeared authenticated: %+v", environment.Tools)
	}
}

func TestBootstrapInitializeRequiresGitAndCancellationNeverWrites(t *testing.T) {
	bootstrapFixtures(t, "dsh")
	called := false
	b := Bootstrap{DataDir: t.TempDir(), initialize: func(context.Context, onboarding.Config) (onboarding.Result, error) {
		called = true
		return onboarding.Result{}, nil
	}}
	replies := bootstrapReplies(t, b, bootstrapInitialize,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"initialize_runner","arguments":{"center_url":"https://center","pairing_code":"code"}}}`)
	if called || !replies[1].Result.IsError {
		t.Fatal("initialization continued without Git")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	if err := b.Serve(ctx, strings.NewReader(bootstrapInitialize), &out); !errors.Is(err, context.Canceled) || out.Len() != 0 {
		t.Fatalf("cancelled bootstrap continued: %q, %v", out.String(), err)
	}
}

func TestBootstrapDiscoversTrustedCustomDefinitionsWithoutModelCommandInput(t *testing.T) {
	bootstrapFixtures(t, "git", "local-custom")
	custom := agent.Definition{ID: "local-custom", Executables: []string{"local-custom"}, Protocol: agent.ProtocolCodex, Cancel: true, Resume: true, ResumeArgs: []string{"resume", "{session}"}, Review: true, ReadOnly: true}
	profilePath := filepath.Join(t.TempDir(), "profile.json")
	replies := bootstrapReplies(t, Bootstrap{Tools: []agent.Definition{custom}, ProfilePath: profilePath}, bootstrapInitialize,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"inspect_environment","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"save_runner_profile","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"initialize_runner","arguments":{"center_url":"https://center.invalid","pairing_code":"unused","tools":[{"id":"injected","executables":["shell"]}]}}}`)
	if replies[1].Result.IsError || replies[2].Result.IsError || !replies[3].Result.IsError {
		t.Fatalf("unexpected custom bootstrap responses: %+v", replies)
	}
	var environment bootstrapEnvironment
	if err := json.Unmarshal([]byte(replies[1].Result.Content[0].Text), &environment); err != nil {
		t.Fatal(err)
	}
	capability := environment.Tools[custom.ID]
	if !capability.Available || !capability.Structured || !capability.Cancel || !capability.Resume || !capability.Review || !capability.ReadOnly || capability.Protocol != custom.Protocol || environment.Profile.Executables[custom.ID] == "" {
		t.Fatal("trusted custom tool not discovered")
	}
	raw, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	var profile BootstrapProfile
	if err := json.Unmarshal(raw, &profile); err != nil {
		t.Fatal(err)
	}
	if len(profile.Tools) != 1 || profile.Tools[0].ID != custom.ID {
		t.Fatal("custom local profile was not retained")
	}
}

func TestBootstrapReuseKeepsCustomExecutableOutsidePath(t *testing.T) {
	toolsDir := bootstrapFixtures(t, "local-custom")
	executable := filepath.Join(toolsDir, "local-custom")
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	custom := agent.Definition{ID: "local-custom", Executables: []string{"not-on-path"}, Protocol: agent.ProtocolCodex, Cancel: true, Review: true}
	dataDir := t.TempDir()
	config := runner.Config{CenterURL: "https://center.invalid", NodeID: "node_existing", Token: "fixture-existing-private-token", Tools: []agent.Definition{custom}, Executables: map[string]string{custom.ID: executable}}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "runner.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	replies := bootstrapReplies(t, Bootstrap{DataDir: dataDir}, bootstrapInitialize,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"inspect_environment","arguments":{}}}`)
	if replies[1].Result.IsError {
		t.Fatal(replies[1])
	}
	var environment bootstrapEnvironment
	text := replies[1].Result.Content[0].Text
	if err := json.Unmarshal([]byte(text), &environment); err != nil {
		t.Fatal(err)
	}
	if !environment.Tools[custom.ID].Available || environment.Profile.Executables[custom.ID] != executable || strings.Contains(text, config.Token) || strings.Contains(text, config.NodeID) {
		t.Fatal("trusted persisted executable was lost or paired identity was disclosed")
	}
}
