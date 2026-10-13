package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"coordica/internal/agent"
	"coordica/internal/localfile"
	"coordica/internal/onboarding"
	"coordica/internal/process"
	"coordica/internal/runner"
)

// BootstrapProfile is local configuration, not a paired Runner identity.
// Authentication and center/node credentials are intentionally absent.
type BootstrapProfile struct {
	Name        string             `json:"name"`
	Platform    string             `json:"platform"`
	Executables map[string]string  `json:"executables"`
	Tools       []agent.Definition `json:"tools,omitempty"`
}

// Bootstrap inspects the local machine without needing a center. Only the
// process owner can select a profile destination, through the startup argument.
type Bootstrap struct {
	ProfilePath string
	ChatProfile string // protected role asset selected by interaction startup
	DataDir     string
	Tools       []agent.Definition // selected only by trusted startup configuration
	Executables map[string]string  // selected only by trusted startup configuration
	// ProvisionChat is bound by trusted interaction startup to saved node state.
	ProvisionChat func(context.Context) error
	initialize    func(context.Context, onboarding.Config) (onboarding.Result, error)
}

type bootstrapExecutable struct {
	Path           string `json:"path"`
	Version        string `json:"version"`
	Available      bool   `json:"available"`
	Authentication string `json:"authentication"`
	Detail         string `json:"detail"`
	Structured     bool   `json:"structured,omitempty"`
	Protocol       string `json:"protocol,omitempty"`
	Cancel         bool   `json:"cancel,omitempty"`
	Resume         bool   `json:"resume,omitempty"`
	Review         bool   `json:"review,omitempty"`
	ReadOnly       bool   `json:"read_only,omitempty"`
}
type bootstrapEnvironment struct {
	OS      string                         `json:"os"`
	Arch    string                         `json:"arch"`
	Host    string                         `json:"host"`
	Git     bootstrapExecutable            `json:"git"`
	Tools   map[string]bootstrapExecutable `json:"tools"`
	Profile BootstrapProfile               `json:"profile"`
}

func (b Bootstrap) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	p, err := b.provider()
	if err != nil {
		return err
	}
	return serve(ctx, in, out, false, p)
}

type bootstrapProvider struct {
	Bootstrap
	registry    *agent.Registry
	tools       []agent.Definition
	executables map[string]string
	inspected   *bootstrapEnvironment
}

func (b Bootstrap) provider() (*bootstrapProvider, error) {
	registry, tools, executables, err := b.localRegistry()
	if err != nil {
		return nil, err
	}
	return &bootstrapProvider{Bootstrap: b, registry: registry, tools: tools, executables: executables}, nil
}
func (p *bootstrapProvider) Tools(context.Context) ([]any, error) {
	return []any{tool("initialize_runner", "Inspect this machine, pair with your chosen center and start the user-level Runner service. The data directory is fixed by the MCP startup configuration; model authentication remains unknown.", map[string]any{
		"center_url": map[string]string{"type": "string"}, "pairing_code": map[string]string{"type": "string"},
		"tls_ca": map[string]string{"type": "string"}, "name": map[string]string{"type": "string"},
		"capacity": map[string]any{"type": "integer", "minimum": 1, "maximum": 2},
	}, []string{"center_url"}),
		tool("inspect_environment", "Inspect OS, architecture, hostname and fixed Git/Agent --version commands. Authentication remains unknown; no credentials are read.", map[string]any{}, nil),
		tool("save_runner_profile", "Save the most recently inspected local configuration to the startup-selected path. Requires inspect_environment first; cannot pair or execute tasks.", map[string]any{}, nil)}, nil
}
func (p *bootstrapProvider) Owns(name string) bool {
	return name == "initialize_runner" || name == "inspect_environment" || name == "save_runner_profile"
}
func (p *bootstrapProvider) Call(ctx context.Context, name string, arguments json.RawMessage) (string, bool) {
	b, registry, tools, executables := p.Bootstrap, p.registry, p.tools, p.executables
	var args struct{}
	var value any
	var err error
	var secrets []string
	if name == "initialize_runner" {
		var config struct {
			CenterURL   string `json:"center_url"`
			PairingCode string `json:"pairing_code"`
			TLSCA       string `json:"tls_ca,omitempty"`
			Name        string `json:"name,omitempty"`
			Capacity    *int   `json:"capacity,omitempty"`
		}
		err = bootstrapArguments(arguments, &config)
		secrets = []string{config.PairingCode}
		capacity := 2
		if config.Capacity != nil {
			capacity = *config.Capacity
		}
		if err == nil && (strings.TrimSpace(config.CenterURL) == "" || capacity < 1 || capacity > 2) {
			err = fmt.Errorf("center_url and capacity 1 or 2 are required; first initialization also requires pairing_code")
		}
		if err == nil && b.DataDir == "" {
			err = fmt.Errorf("runner initialization is disabled; select --runner-data at startup")
		}
		if err == nil {
			p.inspected = nil
			p.inspected, err = inspectRegistry(ctx, registry, tools, executables)
		}
		if err == nil && !p.inspected.Git.Available {
			err = fmt.Errorf("Git version preflight failed; install Git in the MCP process environment before initializing")
		}
		if err == nil {
			name := strings.TrimSpace(config.Name)
			if name == "" {
				name = p.inspected.Profile.Name
			}
			initialize := b.initialize
			if initialize == nil {
				initialize = onboarding.Initialize
			}
			result, initErr := initialize(ctx, onboarding.Config{CenterURL: config.CenterURL, PairingCode: config.PairingCode, TLSCA: config.TLSCA, DataDir: b.DataDir, Name: name, Capacity: capacity, Executables: p.inspected.Profile.Executables, Tools: p.inspected.Profile.Tools})
			value, err = result, initErr
			if result.Registered && b.ProvisionChat != nil {
				if provisionErr := b.ProvisionChat(ctx); provisionErr != nil {
					result.Steps = append(result.Steps, "interaction_chat_pending_retry_initialize_runner")
					if err == nil {
						err = fmt.Errorf("Runner registration retained; limited interaction chat provisioning failed. Check connection and the startup-selected shared profile; conflicting profiles are preserved and inactive own credentials require explicit chat-init --renew. Retry initialize_runner with the saved center after resolving readiness")
					}
				} else {
					result.Steps = append(result.Steps, "interaction_chat_ready")
				}
				value = result
			}
		}
	} else if err = rpcObject(arguments, &args); err == nil {
		switch name {
		case "inspect_environment":
			// A failed refresh cannot authorize saving stale findings.
			p.inspected = nil
			p.inspected, err = inspectRegistry(ctx, registry, tools, executables)
			value = p.inspected
		case "save_runner_profile":
			switch {
			case b.ProfilePath == "":
				err = fmt.Errorf("profile saving is disabled; start with --profile")
			case ValidateProfilePaths(b.ProfilePath, b.DataDir, b.ChatProfile) != nil:
				err = fmt.Errorf("discovery profile conflicts with a protected role asset")
			case p.inspected == nil:
				err = fmt.Errorf("inspect_environment must succeed before saving")
			default:
				err = ctx.Err()
				if err == nil {
					var data []byte
					data, err = json.MarshalIndent(p.inspected.Profile, "", "  ")
					if err == nil {
						err = localfile.WritePrivate(b.ProfilePath, append(data, '\n'))
					}
				}
				value = p.inspected.Profile
			}
		default:
			err = fmt.Errorf("unknown bootstrap tool")
		}
	}
	var text string
	if err != nil {
		if value == nil {
			text = err.Error()
		} else {
			data, _ := json.Marshal(map[string]any{"error": err.Error(), "result": value})
			text = string(data)
		}
	} else {
		data, marshalErr := json.Marshal(value)
		err = marshalErr
		text = string(data)
	}
	raw, _ := json.Marshal(map[string]string{"text": text})
	clean := agent.SanitizeEvent(agent.Event{Type: "bootstrap.result", Data: raw}, secrets...)
	var sanitized map[string]string
	if json.Unmarshal(clean.Data, &sanitized) == nil {
		text = sanitized["text"]
	}
	return text, err != nil
}

// localRegistry reads only startup-selected or already-paired local state. No
// MCP argument can add a command or change a protocol driver definition.
func (b Bootstrap) localRegistry() (*agent.Registry, []agent.Definition, map[string]string, error) {
	tools := b.Tools
	executables := map[string]string{}
	if b.DataDir != "" {
		config, err := runner.Load(b.DataDir)
		if err == nil {
			if tools == nil {
				tools = config.Tools
			}
			for id, path := range config.Executables {
				executables[id] = path
			}
		} else if !os.IsNotExist(err) {
			return nil, nil, nil, err
		}
	}
	registry, err := runner.LocalRegistry(tools)
	if err != nil {
		return nil, nil, nil, err
	}
	// An explicit local registry change may remove old custom definitions.
	for id := range executables {
		if _, registered := registry.Resolve(id); !registered {
			delete(executables, id)
		}
	}
	startupOverrides, err := runner.NormalizeExecutables(registry, b.Executables)
	if err != nil {
		return nil, nil, nil, err
	}
	for id, path := range startupOverrides {
		executables[id] = path
	}
	executables, err = runner.NormalizeExecutables(registry, executables)
	if err != nil {
		return nil, nil, nil, err
	}
	return registry, tools, executables, nil
}

func inspectBootstrap(ctx context.Context) (*bootstrapEnvironment, error) {
	return inspectRegistry(ctx, agent.DefaultRegistry(), nil, nil)
}

func inspectRegistry(ctx context.Context, registry *agent.Registry, tools []agent.Definition, overrides map[string]string) (*bootstrapEnvironment, error) {
	overrides, err := runner.NormalizeExecutables(registry, overrides)
	if err != nil {
		return nil, err
	}
	host, err := os.Hostname()
	if err != nil {
		return nil, err
	}
	result := &bootstrapEnvironment{OS: runtime.GOOS, Arch: runtime.GOARCH, Host: host, Tools: map[string]bootstrapExecutable{}, Profile: BootstrapProfile{Name: host, Platform: runtime.GOOS + "/" + runtime.GOARCH, Executables: map[string]string{}, Tools: tools}}
	result.Git.Authentication = "unknown"
	path, lookupErr := exec.LookPath("git")
	if lookupErr != nil {
		result.Git.Detail = "executable unavailable"
	} else {
		path, err = filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		version, status, probeErr := process.Collect(probeCtx, process.Spec{Executable: path, Args: []string{"--version"}}, 16<<10)
		cancel()
		result.Git.Path, result.Git.Version = path, strings.TrimSpace(string(version))
		result.Git.Available = probeErr == nil && status.ExitCode == 0 && result.Git.Version != ""
		if !result.Git.Available {
			result.Git.Detail = "version preflight failed"
		}
	}
	for _, capability := range registry.Discover(ctx, overrides) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if capability.Executable != "" {
			capability.Executable, err = filepath.Abs(capability.Executable)
			if err != nil {
				return nil, err
			}
		}
		found := bootstrapExecutable{Path: capability.Executable, Version: capability.Version, Available: capability.Available, Authentication: "unknown", Detail: capability.Detail, Structured: capability.Structured, Protocol: capability.Protocol, Cancel: capability.Cancel, Resume: capability.Resume, Review: capability.Review, ReadOnly: capability.ReadOnly}
		if capability.Executable != "" {
			result.Profile.Executables[capability.Tool] = capability.Executable
		}
		raw, _ := json.Marshal(found)
		clean := agent.SanitizeEvent(agent.Event{Type: "bootstrap.version", Data: raw})
		if err := json.Unmarshal(clean.Data, &found); err != nil {
			return nil, err
		}
		result.Tools[capability.Tool] = found
	}
	raw, _ := json.Marshal(result.Git)
	clean := agent.SanitizeEvent(agent.Event{Type: "bootstrap.version", Data: raw})
	if err := json.Unmarshal(clean.Data, &result.Git); err != nil {
		return nil, err
	}
	return result, ctx.Err()
}

func bootstrapArguments(raw []byte, value any) error {
	if err := rpcObject(raw, value); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	for key, field := range fields {
		if bytes.Equal(bytes.TrimSpace(field), []byte("null")) {
			return fmt.Errorf("argument %q cannot be null", key)
		}
	}
	return nil
}

func (b Bootstrap) Inspect(ctx context.Context) (any, error) {
	registry, tools, exe, e := b.localRegistry()
	if e != nil {
		return nil, e
	}
	return inspectRegistry(ctx, registry, tools, exe)
}
