package main

import (
	"bytes"
	"context"
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
	"coordica/internal/domain"
	"coordica/internal/filelock"
	"coordica/internal/gitops"
	"coordica/internal/httpapi"
	"coordica/internal/mcp"
	"coordica/internal/runner"
	"coordica/internal/store"
	"coordica/web"
)

const version = "0.1.1-dev"

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
		return errors.New("usage: coordica serve|pair|runner|ctl|mcp|setup|doctor|tls|restore|version")
	}
	switch args[0] {
	case "version":
		fmt.Println(version)
		return nil
	case "serve":
		return serve(ctx, args[1:])
	case "pair":
		return pair(ctx, args[1:])
	case "runner":
		f := flag.NewFlagSet("runner", flag.ContinueOnError)
		dir := f.String("data", ".coordica-runner", "Runner directory")
		if e := f.Parse(args[1:]); e != nil {
			return e
		}
		cfg, e := runner.Load(*dir)
		if e != nil {
			return e
		}
		// Only the standalone Runner receives the initialization-time PATH.
		// User service managers may otherwise omit Git or npm's Node runtime.
		if cfg.RuntimePath != "" {
			if e = os.Setenv("PATH", cfg.RuntimePath); e != nil {
				return e
			}
		}
		r, e := runner.New(cfg)
		if e != nil {
			return e
		}
		return r.Run(ctx)
	case "ctl":
		return ctl(ctx, args[1:])
	case "mcp":
		return serveMCP(ctx, args[1:])
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
func serve(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("serve", flag.ContinueOnError)
	dir := f.String("data", ".coordica-center", "Center directory")
	addr := f.String("listen", "127.0.0.1:8080", "HTTP(S) listen address")
	cert := f.String("tls-cert", "", "PEM certificate")
	key := f.String("tls-key", "", "PEM private key")
	authFile := f.String("git-auth-file", "", "Center-only file containing HTTPS Authorization header value")
	authRemote := f.String("git-auth-remote", "", "Exact HTTPS repository URL authorized by git-auth-file")
	if e := f.Parse(args); e != nil {
		return e
	}
	host, _, e := net.SplitHostPort(*addr)
	if e != nil {
		return e
	}
	ip := net.ParseIP(host)
	if *cert == "" && (host != "localhost" && (ip == nil || !ip.IsLoopback())) {
		return errors.New("non-loopback listen requires --tls-cert and --tls-key")
	}
	if (*cert == "") != (*key == "") {
		return errors.New("both TLS certificate and key are required")
	}
	absolute, e := filepath.Abs(*dir)
	if e != nil {
		return e
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
	if e = s.RecoverCenter(ctx); e != nil {
		return e
	}
	integration := &app.Integrator{Service: s, Git: g}
	api := &httpapi.Server{Service: s, Integration: integration, UI: web.FS()}
	server := &http.Server{Addr: *addr, Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 90 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
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
	listen, e := net.Listen("tcp", *addr)
	if e != nil {
		cancel()
		workers.Wait()
		return e
	}
	result := make(chan error, 1)
	go func() {
		if *cert != "" {
			result <- server.ServeTLS(listen, *cert, *key)
		} else {
			result <- server.Serve(listen)
		}
	}()
	slog.Info("center ready", "listen", listen.Addr(), "owner_token_file", tokenPath, "version", version)
	select {
	case e = <-result:
	case <-ctx.Done():
		e = nil
	}
	cancel()
	shutdown, done := context.WithTimeout(context.Background(), 10*time.Second)
	defer done()
	_ = server.Shutdown(shutdown)
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
	bootstrap := f.Bool("bootstrap", false, "Local discovery and Runner initialization tools")
	profile := f.String("profile", "", "Fixed output path for local Runner discovery profile")
	dir := f.String("runner-data", "", "Runner directory (bootstrap defaults to user config directory)")
	id := f.String("run", "", "Assigned Run ID")
	generation := f.Int64("generation", 0, "Run generation")
	toolsFile := f.String("tools-file", "", "Trusted local JSON tool definitions file (bootstrap only)")
	var toolExecutables executableFlags
	f.Var(&toolExecutables, "tool-executable", "Tool executable override ID=PATH (bootstrap only, repeatable)")
	if e := f.Parse(args); e != nil {
		return e
	}
	if *bootstrap {
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
		return (mcp.Bootstrap{ProfilePath: *profile, DataDir: absolute, Tools: tools, Executables: toolExecutables}).Serve(ctx, os.Stdin, os.Stdout)
	}
	if *toolsFile != "" || len(toolExecutables) > 0 {
		return errors.New("local tool configuration is supported only with --bootstrap")
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
