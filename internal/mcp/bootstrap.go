package mcp

import (
	"bufio"
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
	DataDir     string
	Tools       []agent.Definition // selected only by trusted startup configuration
	Executables map[string]string  // selected only by trusted startup configuration
	initialize  func(context.Context, onboarding.Config) (onboarding.Result, error)
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
	registry, tools, executables, err := b.localRegistry()
	if err != nil {
		return err
	}
	scan := bufio.NewScanner(in)
	scan.Buffer(make([]byte, 4096), 1<<20)
	enc := json.NewEncoder(out)
	initialized := false
	var inspected *bootstrapEnvironment
	for scan.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var r request
		reply := map[string]any{"jsonrpc": "2.0", "id": json.RawMessage("null")}
		fail := func(code int, message string) { reply["error"] = map[string]any{"code": code, "message": message} }
		switch {
		case !json.Valid(scan.Bytes()):
			fail(-32700, "Parse error")
		case bootstrapStrict(scan.Bytes(), &r) != nil || r.JSONRPC != "2.0" || r.Method == "" || (len(r.ID) > 0 && !validRPCID(r.ID)):
			fail(-32600, "Invalid request")
		case len(r.ID) == 0:
			// JSON-RPC notifications never have a response.
			continue
		default:
			reply["id"] = r.ID
			switch r.Method {
			case "initialize":
				var params struct {
					ProtocolVersion string                     `json:"protocolVersion"`
					Capabilities    map[string]json.RawMessage `json:"capabilities"`
					ClientInfo      map[string]json.RawMessage `json:"clientInfo,omitempty"`
					Meta            map[string]json.RawMessage `json:"_meta,omitempty"`
				}
				if bootstrapStrict(r.Params, &params) != nil || strings.TrimSpace(params.ProtocolVersion) == "" || (params.ClientInfo != nil && !bootstrapClientInfo(params.ClientInfo)) {
					fail(-32602, "Invalid initialization parameters")
					break
				}
				initialized = true
				reply["result"] = map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "coordica-bootstrap", "version": "0.1.0"}}
			case "ping", "tools/list":
				if len(r.Params) > 0 {
					var params struct {
						Meta map[string]json.RawMessage `json:"_meta,omitempty"`
					}
					if bootstrapStrict(r.Params, &params) != nil {
						fail(-32602, "Invalid parameters")
						break
					}
				}
				if r.Method == "ping" {
					reply["result"] = map[string]any{}
				} else if !initialized {
					fail(-32000, "Initialize first")
				} else {
					reply["result"] = map[string]any{"tools": []any{
						tool("initialize_runner", "Inspect this machine, pair with your chosen center and start the user-level Runner service. The data directory is fixed by the MCP startup configuration; model authentication remains unknown.", map[string]any{
							"center_url": map[string]string{"type": "string"}, "pairing_code": map[string]string{"type": "string"},
							"tls_ca": map[string]string{"type": "string"}, "name": map[string]string{"type": "string"},
							"capacity": map[string]any{"type": "integer", "minimum": 1, "maximum": 2},
						}, []string{"center_url", "pairing_code"}),
						tool("inspect_environment", "Inspect OS, architecture, hostname and fixed Git/Agent --version commands. Authentication remains unknown; no credentials are read.", map[string]any{}, nil),
						tool("save_runner_profile", "Save the most recently inspected local configuration to the startup-selected path. Requires inspect_environment first; cannot pair or execute tasks.", map[string]any{}, nil),
					}}
				}
			case "tools/call":
				if !initialized {
					fail(-32000, "Initialize first")
					break
				}
				var call struct {
					Name      string                     `json:"name"`
					Arguments json.RawMessage            `json:"arguments"`
					Meta      map[string]json.RawMessage `json:"_meta,omitempty"`
				}
				if bootstrapStrict(r.Params, &call) != nil || call.Name == "" {
					fail(-32602, "Invalid parameters")
					break
				}
				if len(call.Arguments) == 0 {
					call.Arguments = json.RawMessage(`{}`)
				}
				var args struct{}
				var value any
				var err error
				var secrets []string
				if call.Name == "initialize_runner" {
					var config struct {
						CenterURL   string `json:"center_url"`
						PairingCode string `json:"pairing_code"`
						TLSCA       string `json:"tls_ca,omitempty"`
						Name        string `json:"name,omitempty"`
						Capacity    *int   `json:"capacity,omitempty"`
					}
					err = bootstrapArguments(call.Arguments, &config)
					secrets = []string{config.PairingCode}
					capacity := 2
					if config.Capacity != nil {
						capacity = *config.Capacity
					}
					if err == nil && (strings.TrimSpace(config.CenterURL) == "" || strings.TrimSpace(config.PairingCode) == "" || capacity < 1 || capacity > 2) {
						err = fmt.Errorf("center_url, pairing_code and capacity 1 or 2 are required")
					}
					if err == nil && b.DataDir == "" {
						err = fmt.Errorf("runner initialization is disabled; select --runner-data at startup")
					}
					if err == nil {
						inspected = nil
						inspected, err = inspectRegistry(ctx, registry, tools, executables)
					}
					if err == nil && !inspected.Git.Available {
						err = fmt.Errorf("Git version preflight failed; install Git in the MCP process environment before initializing")
					}
					if err == nil {
						name := strings.TrimSpace(config.Name)
						if name == "" {
							name = inspected.Profile.Name
						}
						initialize := b.initialize
						if initialize == nil {
							initialize = onboarding.Initialize
						}
						value, err = initialize(ctx, onboarding.Config{CenterURL: config.CenterURL, PairingCode: config.PairingCode, TLSCA: config.TLSCA, DataDir: b.DataDir, Name: name, Capacity: capacity, Executables: inspected.Profile.Executables, Tools: inspected.Profile.Tools})
					}
				} else if err = bootstrapStrict(call.Arguments, &args); err == nil {
					switch call.Name {
					case "inspect_environment":
						// A failed refresh cannot authorize saving stale findings.
						inspected = nil
						inspected, err = inspectRegistry(ctx, registry, tools, executables)
						value = inspected
					case "save_runner_profile":
						switch {
						case b.ProfilePath == "":
							err = fmt.Errorf("profile saving is disabled; start with --profile")
						case strings.EqualFold(filepath.Base(b.ProfilePath), "runner.json"):
							err = fmt.Errorf("a bootstrap profile cannot replace paired runner.json identity")
						case inspected == nil:
							err = fmt.Errorf("inspect_environment must succeed before saving")
						default:
							err = ctx.Err()
							if err == nil {
								var data []byte
								data, err = json.MarshalIndent(inspected.Profile, "", "  ")
								if err == nil {
									err = localfile.WritePrivate(b.ProfilePath, append(data, '\n'))
								}
							}
							value = inspected.Profile
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
				reply["result"] = map[string]any{"content": []any{map[string]string{"type": "text", "text": text}}, "isError": err != nil}
			default:
				fail(-32601, "Method not found")
			}
		}
		if err := enc.Encode(reply); err != nil {
			return err
		}
	}
	return scan.Err()
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
	if err := bootstrapStrict(raw, value); err != nil {
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

// Client implementation metadata is descriptive, not an authority source.
// Keep name/version well typed while accepting standard and future metadata.
func bootstrapClientInfo(info map[string]json.RawMessage) bool {
	for _, field := range []string{"name", "version"} {
		var value string
		if json.Unmarshal(info[field], &value) != nil || strings.TrimSpace(value) == "" {
			return false
		}
	}
	return true
}

// Keep the existing strict decoder, adding object and duplicate-key checks.
// In particular, JSON null must not turn into an empty save-tool argument.
func bootstrapStrict(raw []byte, value any) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return fmt.Errorf("parameters must be a JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	if err := bootstrapUnique(decoder); err != nil {
		return err
	}
	return strict(trimmed, value)
}

func bootstrapUnique(decoder *json.Decoder) error {
	first, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, nested := first.(json.Delim)
	if !nested {
		return nil
	}
	keys := map[string]bool{}
	for decoder.More() {
		if delimiter == '{' {
			token, err := decoder.Token()
			if err != nil {
				return err
			}
			key := token.(string)
			if keys[key] {
				return fmt.Errorf("duplicate JSON field %q", key)
			}
			keys[key] = true
		}
		if err := bootstrapUnique(decoder); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}
