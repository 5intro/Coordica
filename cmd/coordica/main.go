package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"coordica/internal/agent"
	"coordica/internal/apiclient"
	"coordica/internal/app"
	"coordica/internal/centerhost"
	"coordica/internal/chatprofile"
	"coordica/internal/domain"
	"coordica/internal/filelock"
	githubhost "coordica/internal/github"
	"coordica/internal/gitops"
	"coordica/internal/httpapi"
	"coordica/internal/mcp"
	"coordica/internal/runner"
	"coordica/internal/store"
	"coordica/web"
)

const version = domain.SourceVersion

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if e := run(ctx, os.Args[1:]); e != nil {
		slog.Error("coordica", "error", e)
		os.Exit(1)
	}
}
func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: coordica serve|pair|runner|ctl|mcp|chat-init|setup|doctor|tls|restore|version")
	}
	switch args[0] {
	case "version":
		fmt.Println(version)
		return nil
	case "host-config":
		return hostConfig(args[1:])
	case "host":
		return host(ctx, args[1:])
	case "center-serve":
		return centerServe(ctx, args[1:])
	case "serve":
		return serve(ctx, args[1:])
	case "pair":
		return pair(ctx, args[1:])
	case "runner":
		return runRunner(ctx, args[1:])
	case "ctl":
		return ctl(ctx, args[1:])
	case "mcp":
		return serveMCP(ctx, args[1:])
	case "chat-init":
		return chatInit(ctx, args[1:])
	case "setup":
		return setup(ctx, args[1:])
	case "doctor":
		return doctor(ctx, args[1:])
	case "tls":
		return certificate(args[1:])
	case "restore":
		return restore(ctx, args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
func serve(ctx context.Context, args []string) error { return serveFixed(ctx, args, nil, "") }
func serveFixed(ctx context.Context, args []string, host *centerhost.Config, hostPath string) error {
	f := flag.NewFlagSet("serve", flag.ContinueOnError)
	dir := f.String("data", ".coordica-center", "Center directory")
	leaderExecutable := f.String("leader-executable", "", "Trusted native Codex executable (gpt-6.1-sol)")
	addr := f.String("listen", "127.0.0.1:8080", "HTTP(S) listen address")
	console := f.String("console-listen", "127.0.0.1:8080", "Machine-local HTTP console address")
	runnerURL := f.String("runner-url", "", "Advertised HTTPS Runner URL (required for wildcard listeners to produce remote setup instructions)")
	runnerConsole := f.String("local-runner-console", "http://127.0.0.1:8081", "Local Runner viewer link (off hides it; does not start a Runner)")
	cert := f.String("tls-cert", "", "PEM certificate")
	key := f.String("tls-key", "", "PEM private key")
	authFile := f.String("git-auth-file", "", "Center-only file containing HTTPS Authorization header value")
	authRemote := f.String("git-auth-remote", "", "Exact HTTPS repository URL authorized by git-auth-file")
	githubRepository := f.String("github-repository", "", "Fixed owner/repository allowed for GitHub PR operations")
	githubTokenFile := f.String("github-token-file", "", "Center-only absolute file containing a GitHub API token")
	if e := f.Parse(args); e != nil {
		return e
	}
	runnerAddr, consoleAddr, shared, e := centerAddresses(*addr, *console, *cert, *key)
	if e != nil {
		return e
	}
	viewerURL, e := localRunnerConsoleURL(*runnerConsole)
	if e != nil {
		return e
	}
	absolute, e := filepath.Abs(*dir)
	if e != nil {
		return e
	}
	if e = centerhost.ValidateCredentialPaths(absolute, []string{*authFile, *githubTokenFile}, *cert, *key); e != nil {
		return e
	}
	var tlsPair tls.Certificate
	if *cert != "" {
		if tlsPair, e = tls.LoadX509KeyPair(*cert, *key); e != nil {
			return e
		}
	}
	if e = os.MkdirAll(absolute, 0700); e != nil {
		return e
	}
	release, e := filelock.Acquire(filepath.Join(absolute, "center.lock"))
	if e != nil {
		return fmt.Errorf("center directory already in use: %w", e)
	}
	defer release()
	db, e := store.Open(filepath.Join(absolute, "coordica.db"))
	if e != nil {
		return e
	}
	defer db.Close()
	s := app.New(db, absolute)
	tokenPath, e := s.Bootstrap(ctx)
	if e != nil {
		return e
	}
	g := gitops.New("git")
	if *authFile != "" {
		raw, e := os.ReadFile(*authFile)
		if e != nil {
			return e
		}
		g, e = g.WithHTTPSAuthorization(*authRemote, strings.TrimSpace(string(raw)))
		if e != nil {
			return e
		}
	}
	var githubAdapter *githubhost.Client
	if *githubRepository != "" || *githubTokenFile != "" {
		githubAdapter, e = githubhost.New(*githubRepository, *githubTokenFile)
		if e != nil {
			return e
		}
		if *authFile == "" {
			remote := "https://github.com/" + *githubRepository + ".git"
			authorization, err := githubAdapter.HTTPSAuthorization(remote)
			if err != nil {
				return err
			}
			// Let the adapter recognize case-equivalent forms of its fixed
			// repository, including before the project has been created.
			g, err = g.WithHTTPSAuthorizationMatching(remote, authorization, githubAdapter.MatchesRepositoryURL)
			if err != nil {
				return err
			}
		}
	}
	if e = s.RecoverCenter(ctx); e != nil {
		return e
	}
	planning := &app.PlanningWorker{Service: s, Executor: CodexPlanningExecutor{Executable: *leaderExecutable}, ExecutionAdapter: "codex"}
	if e = planning.Recover(ctx); e != nil {
		return e
	}
	privatePaths := []string{absolute}
	for _, secret := range []string{*authFile, *githubTokenFile} {
		if secret != "" {
			privatePaths = append(privatePaths, secret)
		}
	}
	if host != nil {
		privatePaths = append(privatePaths, host.ChatProfile, hostPath)
	}
	s.Source = g.WithAnalysisPrivatePaths(privatePaths)
	initialization := &app.InitializationWorker{Service: s, Executor: CodexPlanningExecutor{Executable: *leaderExecutable}}
	if e = initialization.Recover(ctx); e != nil {
		return e
	}
	integration := &app.Integrator{Service: s, Git: g}
	if githubAdapter != nil {
		integration.PullRequests = githubAdapter
	}
	api := &httpapi.Server{Service: s, Integration: integration, UI: web.FS()}
	if host != nil {
		manager := centerManager(hostPath)
		api.Host = manager
	}
	var endpoints []centerEndpoint
	runnerHandler := api.RunnerHandler()
	if shared {
		runnerHandler = api.Handler()
	}
	listen, e := net.Listen("tcp", runnerAddr)
	if e != nil {
		return e
	}
	defer listen.Close()
	endpoints = append(endpoints, centerEndpoint{server: consoleHTTPServer(runnerHandler), listener: listen, cert: *cert, key: *key})
	if !shared {
		local, err := net.Listen("tcp", consoleAddr)
		if err != nil {
			return fmt.Errorf("console listen: %w", err)
		}
		defer local.Close()
		endpoints = append(endpoints, centerEndpoint{server: consoleHTTPServer(api.AdminHandler()), listener: local})
		consoleAddr = local.Addr().String()
	} else {
		consoleAddr = listen.Addr().String()
	}
	api.ConnectionInfo, e = centerConnectionInfo(listen.Addr().String(), consoleAddr, *runnerURL, absolute, tlsPair)
	if e != nil {
		return e
	}
	api.ConnectionInfo.RunnerConsoleURL = viewerURL
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var workers sync.WaitGroup
	start := func(interval time.Duration, fn func(context.Context) error) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			tick := time.NewTicker(interval)
			defer tick.Stop()
			for {
				select {
				case <-workerCtx.Done():
					return
				case <-tick.C:
					if e := fn(workerCtx); e != nil && !errors.Is(e, domain.ErrNotFound) && !errors.Is(e, context.Canceled) {
						slog.Warn("background work", "error", e)
					}
				}
			}
		}()
	}
	// Lease expiry must remain responsive while a long validation command runs.
	start(time.Second, s.Expire)
	start(time.Second, integration.Tick)
	verification := &app.VerificationWorker{Integration: integration}
	start(time.Second, verification.Tick)
	start(time.Second, func(ctx context.Context) error {
		if e := initialization.Tick(ctx); e != nil {
			return e
		}
		return planning.Tick(ctx)
	})
	result := make(chan error, len(endpoints))
	for _, endpoint := range endpoints {
		go func() {
			if endpoint.cert != "" {
				result <- endpoint.server.ServeTLS(endpoint.listener, endpoint.cert, endpoint.key)
			} else {
				result <- endpoint.server.Serve(endpoint.listener)
			}
		}()
	}
	slog.Info("center ready", "listen", listen.Addr(), "console", "http://"+consoleAddr, "owner_token_file", tokenPath, "version", version)
	select {
	case e = <-result:
	case <-ctx.Done():
		e = nil
	}
	cancel()
	shutdown, done := context.WithTimeout(context.Background(), 10*time.Second)
	defer done()
	for _, endpoint := range endpoints {
		if err := endpoint.server.Shutdown(shutdown); err != nil {
			_ = endpoint.server.Close()
		}
	}
	workers.Wait()
	if errors.Is(e, http.ErrServerClosed) {
		return nil
	}
	return e
}
func pair(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("pair", flag.ContinueOnError)
	dir := f.String("data", ".coordica-runner", "Runner directory")
	center := f.String("center", "http://127.0.0.1:8080", "Center URL")
	ca := f.String("ca", "", "Trusted center certificate")
	codeFile := f.String("code-file", "", "File with one-time pairing code (or read stdin)")
	name := f.String("name", "", "Node name")
	capacity := f.Int("capacity", 2, "Parallel runs, 1 or 2")
	codex := f.String("codex", "", "Codex executable (prefer --tool-executable)")
	claude := f.String("claude", "", "Claude executable (prefer --tool-executable)")
	dsh := f.String("dsh", "", "dsh executable (prefer --tool-executable)")
	toolsFile := f.String("tools-file", "", "Trusted local JSON tool definitions file")
	var toolExecutables executableFlags
	f.Var(&toolExecutables, "tool-executable", "Tool executable override ID=PATH (repeatable)")
	profilePath := f.String("profile", "", "Local bootstrap MCP profile")
	if e := f.Parse(args); e != nil {
		return e
	}
	var raw []byte
	var e error
	if *codeFile != "" {
		raw, e = os.ReadFile(*codeFile)
	} else {
		raw, e = io.ReadAll(io.LimitReader(os.Stdin, 4096))
	}
	if e != nil {
		return e
	}
	tools, e := loadToolDefinitions(*toolsFile)
	if e != nil {
		return e
	}
	executables := map[string]string{}
	for id, path := range map[string]string{"codex": *codex, "claude": *claude, "dsh": *dsh} {
		if path != "" {
			executables[id] = path
		}
	}
	for id, path := range toolExecutables {
		executables[id] = path
	}
	if *profilePath != "" {
		raw, e := os.ReadFile(*profilePath)
		if e != nil {
			return e
		}
		profile, e := decodeBootstrapProfile(raw)
		if e != nil {
			return e
		}
		if tools == nil {
			tools = profile.Tools
		}
		if *name == "" {
			*name = profile.Name
		}
		for tool, path := range profile.Executables {
			if executables[tool] == "" {
				executables[tool] = path
			}
		}
	}
	r, e := runner.New(runner.Config{CenterURL: *center, DataDir: *dir, Name: *name, TLSCA: *ca, Capacity: *capacity, Executables: executables, Tools: tools})
	if e != nil {
		return e
	}
	if e = r.Register(ctx, strings.TrimSpace(string(raw))); e != nil {
		return e
	}
	fmt.Println("Runner paired:", r.Config().NodeID)
	return nil
}
func ctl(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("ctl", flag.ContinueOnError)
	center := f.String("center", "http://127.0.0.1:8080", "Center URL")
	ca := f.String("ca", "", "Center CA")
	token := f.String("token-file", ".coordica-center/owner.token", "Owner token file")
	method := f.String("method", "GET", "GET or POST")
	path := f.String("path", "state", "API v1 relative path")
	body := f.String("body-file", "", "JSON input file; - reads stdin")
	key := f.String("key", "", "Stable idempotency key (auto-generated when omitted)")
	if e := f.Parse(args); e != nil {
		return e
	}
	if *method != "GET" && *method != "POST" {
		return errors.New("method must be GET or POST")
	}
	raw, e := os.ReadFile(*token)
	if e != nil {
		return e
	}
	client, e := apiclient.New(*center, string(raw), *ca)
	if e != nil {
		return e
	}
	var input any = map[string]any{}
	if *body != "" {
		var b []byte
		if *body == "-" {
			b, e = io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
		} else {
			b, e = os.ReadFile(*body)
		}
		if e != nil {
			return e
		}
		if e = json.Unmarshal(b, &input); e != nil {
			return e
		}
	}
	if *key == "" && *method == "POST" {
		*key = app.ID("ctl_")
		fmt.Fprintln(os.Stderr, "Idempotency-Key:", *key)
	}
	result, e := client.Do(ctx, *method, "/api/v1/"+*path, *key, input)
	if e != nil {
		return e
	}
	return printJSON(result)
}
func serveMCP(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("mcp", flag.ContinueOnError)
	chatProfile := f.String("chat-profile", "", "Fixed absolute private limited chat profile")
	bootstrap := f.Bool("bootstrap", false, "Deprecated local-only compatibility mode")
	interaction := f.Bool("interaction", false, "Unified local onboarding and limited interaction")
	hostRole := f.String("host-role", "node", "Trusted local host role: center or node")
	hostConfigPath := f.String("host-config", "", "Trusted fixed center configuration path")
	profile := f.String("profile", "", "Fixed output path for local Runner discovery profile")
	dir := f.String("runner-data", "", "Runner directory (bootstrap defaults to user config directory)")
	id := f.String("run", "", "Assigned Run ID")
	generation := f.Int64("generation", 0, "Run generation")
	toolsFile := f.String("tools-file", "", "Trusted local JSON tool definitions file (local interaction/bootstrap only)")
	var toolExecutables executableFlags
	f.Var(&toolExecutables, "tool-executable", "Tool executable override ID=PATH (local interaction/bootstrap only, repeatable)")
	if e := f.Parse(args); e != nil {
		return e
	}
	if f.NArg() != 0 {
		return errors.New("mcp takes flags only")
	}
	if *hostRole != "node" && *hostRole != "center" {
		return errors.New("host-role must be center or node")
	}
	if *hostRole == "center" || *hostConfigPath != "" {
		if !*interaction || *hostRole != "center" || *hostConfigPath == "" || *bootstrap || *id != "" || *generation != 0 {
			return errors.New("center host requires --interaction --host-role center --host-config and no Run identity")
		}
		return serveCenterInteraction(ctx, *hostConfigPath, *chatProfile, *dir, *profile, *toolsFile, toolExecutables)
	}
	if *chatProfile != "" && !*interaction {
		if *bootstrap || *profile != "" || *dir != "" || *id != "" || *generation != 0 || *toolsFile != "" || len(toolExecutables) > 0 {
			return errors.New("chat profile cannot be combined with bootstrap or Run options")
		}
		selected, e := chatprofile.Load(*chatProfile)
		if e != nil {
			return e
		}
		return (mcp.Chat{API: mcp.ProfileCaller{Path: *chatProfile, CenterURL: selected.CenterURL, TLSCA: selected.TLSCA}}).Serve(ctx, os.Stdin, os.Stdout)
	}
	if *bootstrap || *interaction {
		if *id != "" || *generation != 0 || *bootstrap && *interaction {
			return errors.New("interaction/bootstrap cannot be combined with Run identity or each other")
		}
		tools, e := loadToolDefinitions(*toolsFile)
		if e != nil {
			return e
		}
		if *dir == "" {
			base, e := os.UserConfigDir()
			if e != nil {
				return e
			}
			*dir = filepath.Join(base, "Coordica", "runner")
		}
		absolute, e := filepath.Abs(*dir)
		if e != nil {
			return e
		}
		if *profile == "" {
			*profile = filepath.Join(absolute, "discovery.json")
		}
		local := mcp.Bootstrap{ProfilePath: *profile, DataDir: absolute, Tools: tools, Executables: toolExecutables}
		if *interaction {
			if !filepath.IsAbs(*dir) {
				return errors.New("interaction Runner directory must be absolute")
			}
			if *chatProfile == "" {
				base, err := os.UserConfigDir()
				if err != nil {
					return err
				}
				*chatProfile = filepath.Join(base, "Coordica", "interaction.json")
			}
			if !filepath.IsAbs(*chatProfile) || strings.ContainsAny(*chatProfile, "\x00\r\n") {
				return errors.New("absolute interaction profile required")
			}
			if err := mcp.ValidateProfilePaths(*profile, absolute, *chatProfile); err != nil {
				return err
			}
			local.ChatProfile = *chatProfile
			local.ProvisionChat = func(ctx context.Context) error { return provisionNodeInteraction(ctx, absolute, *chatProfile) }
			return (mcp.Interaction{Bootstrap: local, Chat: mcp.Chat{API: mcp.ProfileCaller{Path: *chatProfile, ExpectedScope: domain.ChatScopeNode, RunnerData: absolute}}}).Serve(ctx, os.Stdin, os.Stdout)
		}
		if err := mcp.ValidateProfilePaths(*profile, absolute, ""); err != nil {
			return err
		}
		return local.Serve(ctx, os.Stdin, os.Stdout)
	}
	if *profile != "" || *toolsFile != "" || len(toolExecutables) > 0 {
		return errors.New("local tool configuration is supported only with --bootstrap or --interaction")
	}
	if *dir == "" {
		*dir = ".coordica-runner"
	}
	if *id == "" || strings.ContainsAny(*id, "/.\\") || *generation <= 0 {
		return errors.New("valid --run and --generation required")
	}
	cfg, e := runner.Load(*dir)
	if e != nil {
		return e
	}
	client, e := apiclient.New(cfg.CenterURL, cfg.Token, cfg.TLSCA)
	if e != nil {
		return e
	}
	return (mcp.Server{API: client, RunID: *id, Generation: *generation}).Serve(ctx, os.Stdin, os.Stdout)
}
func restore(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("restore", flag.ContinueOnError)
	source := f.String("from", "", "Backup directory")
	target := f.String("data", "", "New center directory")
	if e := f.Parse(args); e != nil {
		return e
	}
	if *source == "" || *target == "" {
		return errors.New("--from and --data required")
	}
	return app.Restore(ctx, *source, *target, gitops.New("git"))
}
func printJSON(v any) error {
	e := json.NewEncoder(os.Stdout)
	e.SetIndent("", "  ")
	return e.Encode(v)
}

func decodeBootstrapProfile(raw []byte) (mcp.BootstrapProfile, error) {
	var profile mcp.BootstrapProfile
	if len(raw) > 64<<10 {
		return profile, errors.New("bootstrap profile exceeds 64KiB")
	}
	if err := uniqueJSONKeys(json.NewDecoder(bytes.NewReader(raw))); err != nil {
		return profile, err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(&profile); e != nil {
		return profile, e
	}
	if d.Decode(new(any)) != io.EOF {
		return profile, errors.New("invalid profile suffix")
	}
	if profile.Platform != runtime.GOOS+"/"+runtime.GOARCH {
		return profile, errors.New("bootstrap profile belongs to a different platform")
	}
	registry, err := runner.LocalRegistry(profile.Tools)
	if err != nil {
		return profile, err
	}
	profile.Executables, err = runner.NormalizeExecutables(registry, profile.Executables)
	if err != nil {
		return profile, err
	}
	for _, path := range profile.Executables {
		if path != "" && !filepath.IsAbs(path) {
			return profile, errors.New("tool paths must be absolute")
		}
	}
	return profile, nil
}

// Executable flags accept identifiers registered on this machine; registration
// itself remains in a trusted local file, never in center or MCP arguments.
type executableFlags map[string]string

func (v *executableFlags) String() string { return "ID=PATH" }
func (v *executableFlags) Set(raw string) error {
	id, path, ok := strings.Cut(raw, "=")
	if !ok || strings.TrimSpace(path) == "" {
		return errors.New("tool executable must be ID=PATH")
	}
	if err := domain.ValidateToolID(id); err != nil {
		return err
	}
	if *v == nil {
		*v = executableFlags{}
	}
	if _, exists := (*v)[id]; exists {
		return fmt.Errorf("duplicate executable override %q", id)
	}
	(*v)[id] = path
	return nil
}

func loadToolDefinitions(path string) ([]agent.Definition, error) {
	if path == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 1<<20 {
		return nil, errors.New("tool definitions file exceeds 1 MiB")
	}
	if err := uniqueJSONKeys(json.NewDecoder(bytes.NewReader(raw))); err != nil {
		return nil, err
	}
	var definitions []agent.Definition
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&definitions); err != nil {
		return nil, err
	}
	if d.Decode(new(any)) != io.EOF {
		return nil, errors.New("invalid tool definitions suffix")
	}
	if definitions == nil {
		return nil, errors.New("tool definitions must be a JSON array")
	}
	if _, err := runner.LocalRegistry(definitions); err != nil {
		return nil, err
	}
	return definitions, nil
}

func doctor(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("doctor", flag.ContinueOnError)
	toolsFile := f.String("tools-file", "", "Trusted local JSON tool definitions file")
	var overrides executableFlags
	f.Var(&overrides, "tool-executable", "Tool executable override ID=PATH (repeatable)")
	if err := f.Parse(args); err != nil {
		return err
	}
	definitions, err := loadToolDefinitions(*toolsFile)
	if err != nil {
		return err
	}
	registry, err := runner.LocalRegistry(definitions)
	if err != nil {
		return err
	}
	executableMap, err := runner.NormalizeExecutables(registry, overrides)
	if err != nil {
		return err
	}
	return printJSON(registry.Discover(ctx, executableMap))
}

// uniqueJSONKeys validates the trusted local input before typed decoding so
// duplicate definition fields cannot silently replace earlier launch settings.
func uniqueJSONKeys(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, nested := token.(json.Delim)
	if !nested {
		return nil
	}
	seen := map[string]bool{}
	for decoder.More() {
		if delimiter == '{' {
			token, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := token.(string)
			if !ok {
				return errors.New("JSON object key must be a string")
			}
			if seen[key] {
				return fmt.Errorf("duplicate JSON field %q", key)
			}
			seen[key] = true
		}
		if err := uniqueJSONKeys(decoder); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}
