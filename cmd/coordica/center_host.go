package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"coordica/internal/apiclient"
	"coordica/internal/app"
	"coordica/internal/centerhost"
	"coordica/internal/chatprofile"
	"coordica/internal/domain"
	"coordica/internal/mcp"
)

func hostConfig(args []string) error {
	f := flag.NewFlagSet("host-config", flag.ContinueOnError)
	path := f.String("path", "", "Fixed absolute trusted host configuration")
	role := f.String("host-role", "center", "Explicit trusted host role (center)")
	data := f.String("data", "", "New fixed center data instance")
	profile := f.String("chat-profile", "", "Shared limited profile, outside data/repository")
	repository := f.String("repository", "", "Explicit fixed absolute local repository (may be unborn for new project interview)")
	target := f.String("repository-url", "", "Optional fixed target repository HTTPS/bare URL; analysis always uses --repository local source")
	runtimePath := f.String("runtime-path", os.Getenv("PATH"), "Fixed service executable search path")
	branch := f.String("branch", "main", "Fixed repository branch")
	name := f.String("name", "", "Project name")
	executable := f.String("binary", "", "Fixed installed Coordica executable")
	leader := f.String("codex-executable", "", "Fixed native Codex executable; always gpt-6.1-sol")
	console := f.String("console-listen", "127.0.0.1:8080", "Fixed loopback Owner console")
	runner := f.String("runner-listen", "127.0.0.1:8080", "Fixed Runner listener; different address uses generated TLS")
	runnerURL := f.String("runner-url", "", "Fixed HTTPS URL covered by generated TLS")
	checksFile := f.String("checks-file", "", "Optional trusted JSON array of active checks (proposed during baseline analysis, never run during init)")
	proxy := f.String("https-proxy", "", "Optional fixed non-secret standard HTTPS proxy origin for native model network")
	gitAuthFile := f.String("git-auth-file", "", "Optional center-only Git Authorization file")
	gitAuthRemote := f.String("git-auth-remote", "", "Exact HTTPS remote authorized by the Git file")
	githubRepository := f.String("github-repository", "", "Fixed owner/repository for GitHub PR operations")
	githubTokenFile := f.String("github-token-file", "", "Center-only absolute GitHub API token file")
	if e := f.Parse(args); e != nil {
		return e
	}
	if f.NArg() != 0 {
		return errors.New("host-config takes flags only")
	}
	if *executable == "" {
		p, e := os.Executable()
		if e != nil {
			return e
		}
		*executable, e = filepath.EvalSymlinks(p)
		if e != nil {
			return e
		}
	}
	if *leader == "" {
		return errors.New("explicit --codex-executable is required; no model probe is performed")
	}
	checks := []domain.Check{}
	if *checksFile != "" {
		raw, e := os.ReadFile(*checksFile)
		if e != nil {
			return e
		}
		if len(raw) > 64<<10 {
			return errors.New("checks file exceeds limit")
		}
		if e = json.Unmarshal(raw, &checks); e != nil {
			return e
		}
	}
	c := centerhost.Config{Version: 1, Role: *role, Executable: *executable, DataDir: *data, ChatProfile: *profile, Repository: *repository, TargetRepository: *target, Branch: *branch, Name: *name, ConsoleListen: *console, RunnerListen: *runner, RunnerURL: *runnerURL, CodexExecutable: *leader, RuntimePath: *runtimePath, HTTPSProxy: *proxy, Checks: checks}
	c.GitAuthFile, c.GitAuthRemote, c.GitHubRepository, c.GitHubTokenFile = *gitAuthFile, *gitAuthRemote, *githubRepository, *githubTokenFile
	if e := centerhost.Save(*path, c); e != nil {
		return e
	}
	fmt.Println("Trusted center host configuration saved; project will remain paused and unapproved:", *path)
	return nil
}
func host(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("host", flag.ContinueOnError)
	path := f.String("host-config", "", "Trusted fixed center config")
	action := f.String("action", "status", "initialize, start or status")
	if e := f.Parse(args); e != nil {
		return e
	}
	if f.NArg() != 0 {
		return errors.New("host takes flags only")
	}
	manager := centerManager(*path)
	var result centerhost.Status
	var e error
	switch *action {
	case "initialize", "start":
		result, e = manager.Initialize(ctx)
	case "status":
		result, e = manager.Status(ctx)
	default:
		return errors.New("host action must be initialize, start or status")
	}
	if e != nil {
		printJSON(map[string]any{"result": result, "error": e.Error()})
		return e
	}
	return printJSON(result)
}
func centerManager(path string) centerhost.Manager {
	return centerhost.Manager{ConfigPath: path, Provision: provisionCenterInteraction}
}

type localIssuance struct {
	Service *app.Service
	Owner   app.Actor
}

func (c localIssuance) Do(ctx context.Context, method, path, key string, in any) (json.RawMessage, error) {
	if method == "POST" && path == "/api/v1/chat-credentials" {
		req, ok := in.(domain.ChatCredentialRequest)
		if !ok {
			return nil, errors.New("invalid local grant")
		}
		return c.Service.CreateChatCredential(ctx, c.Owner, key, req)
	}
	if method == "POST" && strings.HasPrefix(path, "/api/v1/chat-credentials/") && strings.HasSuffix(path, "/revoke") {
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/api/v1/chat-credentials/"), "/revoke")
		if strings.Contains(id, "/") || !domain.ChatIdentifier.MatchString(id) {
			return nil, errors.New("invalid own limited credential identity")
		}
		return c.Service.RevokeChatCredential(ctx, c.Owner, key, id)
	}
	return nil, errors.New("local issuance boundary permits only limited center credential creation/revocation")
}
func provisionCenterInteraction(ctx context.Context, s *app.Service, c centerhost.Config) error {
	if s == nil {
		raw, e := os.ReadFile(filepath.Join(c.DataDir, "owner.token"))
		if e != nil {
			return e
		}
		api, e := apiclient.New(c.ConsoleURL(), stringTrim(raw), "")
		if e != nil {
			return e
		}
		defer api.CloseIdleConnections()
		_, e = initializeHostChatProfile(ctx, api, chatInitOptions{CenterURL: c.ConsoleURL(), Path: c.ChatProfile, Name: "Shared center host", ChatScope: domain.ChatScopeCenter})
		return e
	}
	verify := func(ctx context.Context, p chatprofile.Profile) (domain.ChatCredentialInfo, error) {
		a, e := s.Authenticate(ctx, p.Token)
		if e != nil {
			return domain.ChatCredentialInfo{}, e
		}
		return s.ChatIdentity(ctx, a)
	}
	_, e := initializeHostChatProfile(ctx, localIssuance{Service: s, Owner: app.Actor{ID: "owner", Role: "owner"}}, chatInitOptions{CenterURL: c.ConsoleURL(), Path: c.ChatProfile, Name: "Shared center host", ChatScope: domain.ChatScopeCenter, Verify: verify})
	return e
}
func stringTrim(raw []byte) string { return strings.TrimSpace(string(raw)) }
func centerServe(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("center-serve", flag.ContinueOnError)
	path := f.String("host-config", "", "Trusted fixed center configuration")
	if e := f.Parse(args); e != nil {
		return e
	}
	if f.NArg() != 0 {
		return errors.New("center-serve takes flags only")
	}
	c, e := centerhost.Load(*path)
	if e != nil {
		return e
	}
	raw, e := os.ReadFile(filepath.Join(c.DataDir, "center-identity.json"))
	if e != nil {
		return errors.New("initialize the trusted center host before service activation")
	}
	var id centerhost.Identity
	if json.Unmarshal(raw, &id) != nil || id.ConfigDigest != centerhost.Digest(c) || id.DataDir != c.DataDir {
		return errors.New("saved center service/data identity conflicts")
	}
	argv := []string{"--data", c.DataDir, "--listen", c.RunnerListen, "--console-listen", c.ConsoleListen, "--leader-executable", c.CodexExecutable}
	if c.GitAuthFile != "" {
		argv = append(argv, "--git-auth-file", c.GitAuthFile, "--git-auth-remote", c.GitAuthRemote)
	}
	if c.GitHubTokenFile != "" {
		argv = append(argv, "--github-repository", c.GitHubRepository, "--github-token-file", c.GitHubTokenFile)
	}
	if c.TLS() {
		argv = append(argv, "--tls-cert", filepath.Join(c.DataDir, "center.crt"), "--tls-key", filepath.Join(c.DataDir, "center.key"))
		u, _ := url.Parse(c.RunnerURL)
		ip := net.ParseIP(u.Hostname())
		if ip == nil && u.Hostname() != "localhost" || ip != nil && !ip.IsLoopback() {
			argv = append(argv, "--runner-url", c.RunnerURL)
		}
	}
	if e = os.Setenv("PATH", c.RuntimePath); e != nil {
		return e
	}
	if c.HTTPSProxy != "" {
		if e = os.Setenv("HTTPS_PROXY", c.HTTPSProxy); e != nil {
			return e
		}
	}
	return serveFixed(ctx, argv, &c, *path)
}

func serveCenterInteraction(ctx context.Context, path, profile, runnerData, discovery string, toolsFile string, executables executableFlags) error {
	c, e := centerhost.Load(path)
	if e != nil {
		return e
	}
	if profile != "" && profile != c.ChatProfile {
		return errors.New("chat profile conflicts with trusted center host")
	}
	tools, e := loadToolDefinitions(toolsFile)
	if e != nil {
		return e
	}
	if executables == nil {
		executables = executableFlags{}
	}
	if chosen := executables["codex"]; chosen != "" && chosen != c.CodexExecutable {
		return errors.New("native Codex discovery executable conflicts with trusted center")
	}
	executables["codex"] = c.CodexExecutable
	if runnerData == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return err
		}
		runnerData = filepath.Join(base, "Coordica", "runner")
	}
	if !filepath.IsAbs(runnerData) {
		return errors.New("absolute fixed center Runner directory required")
	}
	if discovery == "" {
		discovery = filepath.Join(runnerData, "discovery.json")
	}
	if err := mcp.ValidateCenterPaths(discovery, runnerData, c.ChatProfile, c.DataDir, path, c.Executable, c.CodexExecutable, toolsFile); err != nil {
		return err
	}
	local := mcp.Bootstrap{Tools: tools, Executables: executables, DataDir: runnerData, ProfilePath: discovery, ChatProfile: c.ChatProfile,
		ProvisionChat: func(ctx context.Context) error { return provisionNodeInteraction(ctx, runnerData, c.ChatProfile) }}
	return (mcp.Interaction{Center: &mcp.CenterLocal{Manager: centerManager(path)}, Bootstrap: local, Chat: mcp.Chat{API: mcp.ProfileCaller{Path: c.ChatProfile, ExpectedScope: domain.ChatScopeCenter, CenterURL: c.ConsoleURL()}}}).Serve(ctx, os.Stdin, os.Stdout)
}
