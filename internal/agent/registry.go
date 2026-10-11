package agent

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"coordica/internal/process"
)

const (
	ProtocolCodex  = "codex-jsonl"
	ProtocolClaude = "claude-stream-json"
	ProtocolACP    = "acp"
	ProtocolPi     = "pi-rpc"
)

// Definition is trusted local launch configuration, never a command supplied
// by a center task. Protocol selects a shared driver; ID is only an identity.
// Argument templates are literal argv entries, not shell expressions.
type Definition struct {
	ID              string   `json:"id"`
	Aliases         []string `json:"aliases,omitempty"`
	Executables     []string `json:"executables"`
	VersionArgs     []string `json:"version_args,omitempty"`
	Protocol        string   `json:"protocol"`
	StartArgs       []string `json:"start_args,omitempty"`
	DevelopmentArgs []string `json:"development_args,omitempty"`
	ReviewArgs      []string `json:"review_args,omitempty"`
	ResumeArgs      []string `json:"resume_args,omitempty"`
	PromptArgs      []string `json:"prompt_args,omitempty"`
	Resume          bool     `json:"resume"`
	Cancel          bool     `json:"cancel"`
	Review          bool     `json:"review"`
	ReadOnly        bool     `json:"read_only"`
	Approvals       string   `json:"approvals"`
	ResumeMethod    string   `json:"resume_method,omitempty"`
	CloseMethod     string   `json:"close_method,omitempty"`
	ReviewMode      string   `json:"review_mode,omitempty"`
	AuthMethod      string   `json:"auth_method,omitempty"`
	MinimumVersion  string   `json:"minimum_version,omitempty"`
	CommitMode      string   `json:"commit_mode,omitempty"`
}

type Registry struct {
	mu          sync.RWMutex
	definitions []Definition
	names       map[string]int
}

var validID = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

func NewRegistry(definitions ...Definition) (*Registry, error) {
	r := &Registry{names: map[string]int{}}
	for _, definition := range definitions {
		if err := r.Register(definition); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// DefaultRegistry returns a fresh registry. Local extensions cannot change
// another Runner's defaults or a later bootstrap request.
func DefaultRegistry() *Registry {
	r, err := NewRegistry(BuiltinDefinitions()...)
	if err != nil {
		panic(err)
	}
	return r
}

func (r *Registry) Register(def Definition) error {
	if !validID.MatchString(def.ID) {
		return errors.New("invalid tool ID")
	}
	if def.CommitMode != "" && def.CommitMode != "agent" && def.CommitMode != "runner" {
		return errors.New("commit mode must be agent or runner")
	}
	if def.Protocol != ProtocolCodex && def.Protocol != ProtocolClaude && def.Protocol != ProtocolACP && def.Protocol != ProtocolPi {
		return fmt.Errorf("unsupported protocol driver %q", def.Protocol)
	}
	if len(def.Executables) == 0 {
		return errors.New("at least one executable candidate is required")
	}
	if def.ReadOnly && !def.Review {
		return errors.New("read-only restriction requires review capability")
	}
	if def.MinimumVersion != "" && !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(def.MinimumVersion) {
		return errors.New("minimum version must be a numeric major.minor.patch")
	}
	if def.Protocol == ProtocolPi && (def.MinimumVersion == "" || !versionAtLeast(def.MinimumVersion, "1.1.0")) {
		return errors.New("pi RPC requires a protocol baseline of at least 1.1.0")
	}
	if def.Resume && def.Protocol != ProtocolACP && len(def.ResumeArgs) == 0 {
		return errors.New("resume capability requires explicit resume arguments")
	}
	if def.Protocol != ProtocolACP && (def.ResumeMethod != "" || def.CloseMethod != "" || def.ReviewMode != "" || def.AuthMethod != "") {
		return errors.New("ACP session methods require the ACP driver")
	}
	if def.Protocol == ProtocolACP && def.Resume && def.ResumeMethod != "session/load" && def.ResumeMethod != "session/resume" {
		return errors.New("ACP resume requires an explicit supported resume method")
	}
	if def.CloseMethod != "" && def.CloseMethod != "session/close" {
		return errors.New("unsupported ACP close method")
	}
	for _, values := range [][]string{def.Executables, def.VersionArgs, def.StartArgs, def.DevelopmentArgs, def.ReviewArgs, def.ResumeArgs, def.PromptArgs} {
		for _, value := range values {
			if value == "" || strings.IndexByte(value, 0) >= 0 {
				return errors.New("empty or NUL launch argument")
			}
			if strings.ContainsAny(value, "{}") && value != "{session}" && value != "{allowed_tools}" {
				return errors.New("unsupported argument template")
			}
		}
	}
	if len(def.VersionArgs) == 0 {
		def.VersionArgs = []string{"--version"}
	}
	names := append([]string{def.ID}, def.Aliases...)
	seen := map[string]bool{}
	for _, name := range names {
		if !validID.MatchString(name) || seen[name] {
			return errors.New("invalid or duplicate tool alias")
		}
		seen[name] = true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, name := range names {
		if _, found := r.names[name]; found {
			return fmt.Errorf("tool identity %q is already registered", name)
		}
	}
	for _, name := range names {
		r.names[name] = len(r.definitions)
	}
	r.definitions = append(r.definitions, cloneDefinition(def))
	return nil
}

func cloneDefinition(def Definition) Definition {
	def.Aliases = append([]string(nil), def.Aliases...)
	def.Executables = append([]string(nil), def.Executables...)
	def.VersionArgs = append([]string(nil), def.VersionArgs...)
	def.StartArgs = append([]string(nil), def.StartArgs...)
	def.DevelopmentArgs = append([]string(nil), def.DevelopmentArgs...)
	def.ReviewArgs = append([]string(nil), def.ReviewArgs...)
	def.ResumeArgs = append([]string(nil), def.ResumeArgs...)
	def.PromptArgs = append([]string(nil), def.PromptArgs...)
	return def
}

func (r *Registry) Resolve(id string) (Definition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	i, ok := r.names[strings.ToLower(strings.TrimSpace(id))]
	if !ok {
		return Definition{}, false
	}
	return cloneDefinition(r.definitions[i]), true
}

func (r *Registry) Definitions() []Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]Definition, len(r.definitions))
	for i, def := range r.definitions {
		defs[i] = cloneDefinition(def)
	}
	return defs
}

func (r *Registry) Discover(ctx context.Context, executables map[string]string) []Capability {
	var caps []Capability
	for _, def := range r.Definitions() {
		caps = append(caps, r.Probe(ctx, def.ID, executables[def.ID]))
	}
	return caps
}

// Probe is version-only discovery. Declared protocol capabilities are not a
// certification of authentication, installed protocol version, or OS support.
func (r *Registry) Probe(ctx context.Context, tool, executable string) Capability {
	def, found := r.Resolve(tool)
	c := Capability{Tool: strings.ToLower(strings.TrimSpace(tool))}
	if !found {
		c.Detail = "unregistered tool"
		return c
	}
	c.Tool, c.Protocol, c.Resume, c.Cancel, c.Review, c.ReadOnly, c.Approvals = def.ID, def.Protocol, def.Resume, def.Cancel, def.Review, def.ReadOnly, def.Approvals
	path, err := resolveExecutable(def, executable)
	if err != nil {
		c.Detail = "executable unavailable"
		return c
	}
	c.Executable = path
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, result, err := process.Collect(probeCtx, process.Spec{Executable: path, Args: def.VersionArgs, Env: developmentEnv()}, 16<<10)
	if errors.Is(err, process.ErrUnsafeBatchCommand) {
		c.Detail = "Windows batch installation path is unsupported; use a path without shell metacharacters or a native .exe"
		return c
	}
	if err != nil || result.ExitCode != 0 {
		c.Detail = "version preflight failed"
		return c
	}
	c.Version = strings.TrimSpace(string(out))
	if c.Version == "" {
		c.Detail = "version preflight returned no version"
		return c
	}
	if def.MinimumVersion != "" && !versionAtLeast(c.Version, def.MinimumVersion) {
		c.Detail = "installed version is older than the registered protocol baseline " + def.MinimumVersion
		return c
	}
	c.Available, c.Structured = true, true
	c.Detail = "version detected; protocol/capabilities declared by local registration; authentication and OS/tool execution not certified"
	return c
}

func resolveExecutable(def Definition, executable string) (string, error) {
	resolve := func(name string) (string, error) {
		path, err := exec.LookPath(name)
		if err != nil {
			return "", err
		}
		return filepath.Abs(path)
	}
	if executable != "" {
		return resolve(executable)
	}
	for _, name := range def.Executables {
		if path, err := resolve(name); err == nil {
			return path, nil
		}
	}
	return "", exec.ErrNotFound
}

func launchArgs(def Definition, req Request) []string {
	args := append([]string(nil), def.StartArgs...)
	if isReview(req) {
		args = append(args, def.ReviewArgs...)
	} else {
		args = append(args, def.DevelopmentArgs...)
	}
	if req.ResumeSession != "" {
		args = append(args, def.ResumeArgs...)
	}
	args = append(args, def.PromptArgs...)
	tools := req.AllowedTools
	if len(tools) == 0 {
		tools = []string{"Read", "Glob", "Grep"}
	}
	for i, value := range args {
		switch value {
		case "{session}":
			args[i] = req.ResumeSession
		case "{allowed_tools}":
			args[i] = strings.Join(tools, ",")
		}
	}
	return args
}

func isReview(req Request) bool { return req.Role == "review" || req.Role == "reviewer" }

// BuiltinDefinitions is the only built-in tool list. New compatible tools use
// Register rather than changing discovery, task validation, or scheduling.
func BuiltinDefinitions() []Definition {
	return []Definition{
		{ID: "codex", Executables: []string{"codex"}, Protocol: ProtocolCodex, StartArgs: []string{"--ask-for-approval", "never", "exec", "--json"}, DevelopmentArgs: []string{"--sandbox", "workspace-write"}, ReviewArgs: []string{"--sandbox", "read-only"}, ResumeArgs: []string{"resume", "{session}"}, PromptArgs: []string{"--", "-"}, Resume: true, Cancel: true, Review: true, ReadOnly: true, Approvals: "no interactive approval; sandbox-limited execution"},
		{ID: "claude", Aliases: []string{"claude-code"}, Executables: []string{"claude"}, Protocol: ProtocolClaude, StartArgs: []string{"-p", "--output-format", "stream-json", "--verbose", "--permission-mode", "dontAsk"}, DevelopmentArgs: []string{"--allowedTools", "{allowed_tools}"}, ReviewArgs: []string{"--allowedTools", "Read,Glob,Grep", "--tools", "Read,Glob,Grep"}, ResumeArgs: []string{"--resume", "{session}"}, Resume: true, Cancel: true, Review: true, ReadOnly: true, Approvals: "dontAsk with explicit tool allow rules; native Windows has no built-in sandbox"},
		{ID: "dsh", CommitMode: "runner", Aliases: []string{"deepseek-harness"}, Executables: []string{"dsh"}, Protocol: ProtocolACP, StartArgs: []string{"--profile", "acp"}, Resume: true, ResumeMethod: "session/resume", CloseMethod: "session/close", Cancel: true, Review: true, Approvals: "unverifiable ACP permission requests are rejected; no native read-only sandbox"},
		{ID: "cursor", CommitMode: "runner", Aliases: []string{"cursor-agent"}, Executables: []string{"cursor-agent", "agent"}, Protocol: ProtocolACP, StartArgs: []string{"acp"}, Resume: true, ResumeMethod: "session/load", AuthMethod: "cursor_login", ReviewMode: "ask", Cancel: true, Review: true, ReadOnly: true, Approvals: "ACP permissions are denied unless independently verifiable; review requires advertised ask mode"},
		{ID: "kimi", CommitMode: "runner", Aliases: []string{"kimi-code", "kimicode"}, Executables: []string{"kimi"}, Protocol: ProtocolACP, StartArgs: []string{"acp"}, Resume: true, ResumeMethod: "session/load", CloseMethod: "session/close", Cancel: true, Review: true, Approvals: "unverifiable ACP permission requests are rejected; no native read-only sandbox"},
		{ID: "pi", Executables: []string{"pi"}, Protocol: ProtocolPi, StartArgs: []string{"--mode", "rpc"}, Resume: true, ResumeArgs: []string{"--session", "{session}"}, MinimumVersion: "1.1.0", Cancel: true, Review: true, ReviewArgs: []string{"--tools", "read,grep,find,ls", "--no-extensions", "--no-skills", "--no-prompt-templates", "--no-mcp"}, ReadOnly: true, Approvals: "developer tools use local authenticated policy; review removes write and execution tools and extensions"},
	}
}
