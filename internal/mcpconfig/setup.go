// Package mcpconfig registers one local Coordica MCP launcher with client
// applications. It knows nothing about pairing, task state, or Agent execution.
package mcpconfig

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"coordica/internal/process"
)

type Options struct {
	Client, Executable, RunnerData, ToolsFile string
	Mode, ChatProfile                         string
	HostRole, HostConfig                      string
	Home, ConfigHome, OutputDir               string
	Apply                                     bool
}

type Result struct {
	Client  string `json:"client"`
	Status  string `json:"status"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message,omitempty"`
	// Content contains only a newly generated launcher, never existing settings.
	Content string `json:"content,omitempty"`
	// Internal provenance for host-wide path agreement before auto apply.
	existingRunner, existingChat, existingRole, existingHost string
}

type dependencies struct {
	lookPath func(string) (string, error)
	command  func(context.Context, string, []string, string) ([]byte, error)
	getenv   func(string) string
}

// Run previews by default. Apply changes only the selected clients' Coordica
// launcher; it never invokes the launcher or starts a Runner.
func Run(ctx context.Context, options Options) ([]Result, error) {
	return run(ctx, options, dependencies{lookPath: exec.LookPath, command: command, getenv: os.Getenv})
}

func command(ctx context.Context, executable string, args []string, dir string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, status, err := process.Collect(ctx, process.Spec{Executable: executable, Args: args, Dir: dir}, maxConfigBytes)
	if err != nil || status.ExitCode != 0 || status.Canceled || !status.TreeStopped {
		// CLI diagnostics may include configuration or credentials. Do not put
		// them in a result, even if a malformed client prints them to stderr.
		return nil, errors.New("client command failed or timed out; configuration was not confirmed")
	}
	return out, nil
}

func run(ctx context.Context, options Options, deps dependencies) ([]Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(options.Executable) || strings.ContainsAny(options.Executable, "\x00\r\n") {
		return nil, errors.New("a fixed absolute Coordica executable path is required")
	}
	if options.RunnerData != "" && (!filepath.IsAbs(options.RunnerData) || strings.ContainsAny(options.RunnerData, "\x00\r\n")) {
		return nil, errors.New("Runner data directory must be absolute")
	}
	if options.ToolsFile != "" && (!filepath.IsAbs(options.ToolsFile) || strings.ContainsAny(options.ToolsFile, "\x00\r\n")) {
		return nil, errors.New("tool definitions file must be absolute")
	}
	if options.HostRole != "" && options.HostRole != "center" && options.HostRole != "node" {
		return nil, errors.New("host role must be center or node")
	}
	if options.HostConfig != "" && (!filepath.IsAbs(options.HostConfig) || strings.ContainsAny(options.HostConfig, "\x00\r\n")) {
		return nil, errors.New("host configuration must be absolute")
	}
	if options.HostRole == "center" && (options.HostConfig == "") || options.HostRole != "center" && options.HostConfig != "" {
		return nil, errors.New("center requires fixed host config; node cannot select center config")
	}
	if options.Mode == "" {
		options.Mode = "interaction"
	}
	if options.Mode != "interaction" && options.Mode != "bootstrap" && options.Mode != "chat" {
		return nil, errors.New("mode must be interaction, bootstrap or chat")
	}
	if options.Mode == "chat" {
		if !filepath.IsAbs(options.ChatProfile) || strings.ContainsAny(options.ChatProfile, "\x00\r\n") || options.RunnerData != "" || options.ToolsFile != "" {
			return nil, errors.New("chat mode requires an absolute profile and no Runner options")
		}
	} else if options.Mode == "bootstrap" && options.ChatProfile != "" {
		return nil, errors.New("chat profile requires --mode chat")
	}
	if options.ChatProfile != "" && (!filepath.IsAbs(options.ChatProfile) || strings.ContainsAny(options.ChatProfile, "\x00\r\n")) {
		return nil, errors.New("chat profile must be absolute")
	}
	if options.Client == "" {
		options.Client = "auto"
	}
	clients := []string{"claude", "codex", "cursor", "kimi", "pi", "dsh"}
	if options.Client != "auto" {
		known := options.Client == "generic"
		for _, client := range clients {
			known = known || options.Client == client
		}
		if !known {
			return nil, errors.New("unknown MCP client")
		}
		clients = []string{options.Client}
	}
	var err error
	if options.Home == "" {
		options.Home, err = os.UserHomeDir()
		if err != nil {
			return nil, err
		}
	}
	if options.ConfigHome == "" {
		options.ConfigHome, err = os.UserConfigDir()
		if err != nil {
			return nil, err
		}
	}
	options.Home, err = canonicalBase(options.Home)
	if err != nil {
		return nil, err
	}
	options.ConfigHome, err = canonicalBase(options.ConfigHome)
	if err != nil {
		return nil, err
	}
	if options.OutputDir == "" {
		options.OutputDir = filepath.Join(options.ConfigHome, "Coordica", "mcp")
	} else {
		options.OutputDir, err = canonicalBase(options.OutputDir)
		if err != nil {
			return nil, err
		}
	}
	results := make([]Result, 0, len(clients))
	for _, client := range clients {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		if options.Client == "auto" && !discovered(options, client, deps) {
			continue
		}
		preview := options
		if options.Mode == "interaction" && options.Client == "auto" {
			preview.Apply = false
		}
		results = append(results, configure(ctx, preview, client, deps))
	}
	if options.Mode == "interaction" && options.Client == "auto" && len(results) > 0 {
		// A failed preflight must not leave other clients partly migrated.
		for _, result := range results {
			if result.Status == "error" || result.Status == "conflict" || result.Status == "unsupported" {
				return results, nil
			}
		}
		for _, result := range results {
			for _, pair := range []struct {
				selected *string
				existing string
			}{{&options.RunnerData, result.existingRunner}, {&options.ChatProfile, result.existingChat}, {&options.HostRole, result.existingRole}, {&options.HostConfig, result.existingHost}} {
				if pair.existing != "" {
					if err := preservePath(pair.selected, pair.existing); err != nil {
						return []Result{{Client: options.Client, Status: "conflict", Message: "Existing host interaction paths differ across selected clients; all configurations left unchanged. Select matching paths or resolve conflicting registrations explicitly."}}, nil
					}
				}
			}
		}
		for i, result := range results {
			results[i] = configure(ctx, options, result.Client, deps)
		}
	}
	if len(results) == 0 {
		results = append(results, Result{Client: "auto", Status: "not_found", Message: "No supported client installation was detected. Select --client explicitly or export --client generic."})
	}
	return results, nil
}

func discovered(options Options, client string, deps dependencies) bool {
	if _, err := deps.lookPath(client); err == nil {
		return true
	}
	variables := map[string]string{"claude": "CLAUDE_CONFIG_DIR", "codex": "CODEX_HOME", "kimi": "KIMI_CODE_HOME", "pi": "PI_CODING_AGENT_DIR"}
	if deps.getenv != nil && variables[client] != "" && deps.getenv(variables[client]) != "" {
		return true
	}
	paths := map[string][]string{
		"claude": {".claude.json", ".claude"}, "codex": {".codex"},
		"cursor": {".cursor"}, "kimi": {".kimi", ".kimi-code"}, "pi": {filepath.Join(".pi", "agent")},
	}
	for _, relative := range paths[client] {
		path := filepath.Join(options.Home, relative)
		if noLinks(options.Home, path) == nil {
			if _, err := os.Lstat(path); err == nil {
				return true
			}
		}
	}
	return false
}

func launcher(options Options, withType bool) []byte {
	entry := map[string]any{"command": options.Executable, "args": arguments(options)}
	if withType {
		entry["type"] = "stdio"
	}
	raw, _ := json.MarshalIndent(entry, "", "  ")
	return raw
}

func arguments(options Options) []string {
	if options.Mode == "chat" {
		return []string{"mcp", "--chat-profile", options.ChatProfile}
	}
	if options.Mode == "" || options.Mode == "interaction" {
		options = interactionDefaults(options)
		args := []string{"mcp", "--interaction"}
		if options.HostRole == "center" {
			args = append(args, "--host-role", "center", "--host-config", options.HostConfig, "--runner-data", options.RunnerData)
		} else {
			args = append(args, "--runner-data", options.RunnerData)
			if options.HostRole == "node" {
				args = append(args, "--host-role", "node")
			}
		}
		args = append(args, "--chat-profile", options.ChatProfile)
		if options.ToolsFile != "" {
			args = append(args, "--tools-file", options.ToolsFile)
		}
		return args
	}
	args := []string{"mcp", "--bootstrap"}
	if options.RunnerData != "" {
		args = append(args, "--runner-data", options.RunnerData)
	}
	if options.ToolsFile != "" {
		args = append(args, "--tools-file", options.ToolsFile)
	}
	return args
}

func configure(ctx context.Context, options Options, client string, deps dependencies) Result {
	switch client {
	case "codex":
		return configureCodex(ctx, options, deps)
	case "dsh", "generic":
		return exportConfig(options, client)
	case "claude":
		base, err := clientBase(options.Home, "CLAUDE_CONFIG_DIR", deps)
		if err != nil {
			return Result{Client: client, Status: "error", Message: err.Error()}
		}
		return configureJSON(options, client, base, filepath.Join(base, ".claude.json"))
	case "cursor":
		return configureJSON(options, client, options.Home, filepath.Join(options.Home, ".cursor", "mcp.json"))
	case "kimi", "pi":
		return configureVersioned(ctx, options, client, deps)
	}
	return Result{Client: client, Status: "unsupported", Message: "No verified configuration format is available."}
}

func clientBase(fallback, variable string, deps dependencies) (string, error) {
	if deps.getenv == nil || deps.getenv(variable) == "" {
		return fallback, nil
	}
	return canonicalBase(deps.getenv(variable))
}

func configureJSON(options Options, client, base, path string) Result {
	entry := launcher(options, client == "claude" || client == "cursor")
	result := Result{Client: client, Path: path, Content: string(entry)}
	before, err := readSnapshot(base, path)
	if err != nil {
		result.Status, result.Message = "error", err.Error()
		return result
	}
	if before.info != nil && len(before.data) == 0 {
		result.Status, result.Message = "error", "Existing configuration is empty; left unchanged."
		return result
	}
	var merged []byte
	var same bool
	if options.Mode == "interaction" {
		merged, options, same, err = mergeInteractionJSON(before.data, options, client == "claude" || client == "cursor")
		result.Content = string(launcher(options, client == "claude" || client == "cursor"))
	} else {
		merged, same, err = mergeNamedJSON(before.data, entry, serverName(options))
	}
	if err != nil {
		result.Status, result.Message = "conflict", err.Error()
		return result
	}
	if options.Mode == "interaction" {
		var root, servers map[string]json.RawMessage
		if object(before.data, &root) == nil && object(root["mcpServers"], &servers) == nil {
			if servers["coordica"] != nil {
				result.existingRunner = options.RunnerData
				result.existingRole, result.existingHost = options.HostRole, options.HostConfig
			}
			if servers["coordica-chat"] != nil || servers["coordica"] != nil && isInteractionLauncher(servers["coordica"], options) {
				result.existingChat = options.ChatProfile
			}
		}
	}
	if same {
		result.Status, result.Message = "unchanged", "The existing Coordica launcher matches. Other client options were retained; check that the service is enabled and trusted in the client."
		return result
	}
	if !options.Apply {
		result.Status, result.Message = "preview", "Will merge mcpServers."+serverName(options)+" and preserve other settings; an existing file will be backed up."
		if options.Mode == "interaction" {
			result.Message = interactionPlan
		}
		return result
	}
	backup, err := writeConfig(base, path, before, merged)
	if err != nil {
		result.Status, result.Message = "error", err.Error()
		return result
	}
	result.Status, result.Message = "installed", "MCP launcher registered. Reload the client and confirm its MCP tools."
	if backup != "" {
		result.Message += " Previous configuration backup: " + backup
	}
	return result
}

func exportConfig(options Options, client string) Result {
	if options.Mode == "interaction" {
		return exportInteraction(options, client)
	}
	name := serverName(options) + ".mcp.json"
	content := exportContent(options, client)
	message := "Import this JSON using the client's MCP configuration interface."
	if client == "dsh" {
		name = serverName(options) + ".cordis.yml"
		message = "Load this overlay with dsh web --patch followed by this file's quoted absolute path. Existing dsh profiles were not modified."
	}
	path := filepath.Join(options.OutputDir, name)
	result := Result{Client: client, Path: path, Content: string(content)}
	base := options.ConfigHome
	if relative, err := filepath.Rel(base, options.OutputDir); err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		base = options.OutputDir
	}
	before, err := readSnapshot(base, path)
	if err != nil {
		result.Status, result.Message = "error", err.Error()
		return result
	}
	if before.info != nil {
		if string(before.data) == string(content) {
			result.Status, result.Message = "unchanged", message
		} else {
			result.Status, result.Message = "conflict", "A different exported configuration already exists. Select a new --output directory; the file was left unchanged."
		}
		return result
	}
	if !options.Apply {
		result.Status, result.Message = "preview", "Will export a configuration file. "+message
		return result
	}
	if _, err := writeConfig(base, path, before, content); err != nil {
		result.Status, result.Message = "error", err.Error()
		return result
	}
	result.Status, result.Message = "installed", message
	return result
}

func serverName(options Options) string {
	if options.Mode == "chat" {
		return "coordica-chat"
	}
	return "coordica"
}
