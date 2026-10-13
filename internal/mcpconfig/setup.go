// Package mcpconfig registers one local Coordica MCP launcher with client
// applications. It knows nothing about pairing, task state, or Agent execution.
package mcpconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	if options.Mode == "" {
		options.Mode = "bootstrap"
	}
	if options.Mode != "bootstrap" && options.Mode != "chat" {
		return nil, errors.New("mode must be bootstrap or chat")
	}
	if options.Mode == "chat" {
		if !filepath.IsAbs(options.ChatProfile) || strings.ContainsAny(options.ChatProfile, "\x00\r\n") || options.RunnerData != "" || options.ToolsFile != "" {
			return nil, errors.New("chat mode requires an absolute profile and no Runner options")
		}
	} else if options.ChatProfile != "" {
		return nil, errors.New("chat profile requires --mode chat")
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
		results = append(results, configure(ctx, options, client, deps))
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
	merged, same, err := mergeNamedJSON(before.data, entry, serverName(options))
	if err != nil {
		result.Status, result.Message = "conflict", err.Error()
		return result
	}
	if same {
		result.Status, result.Message = "unchanged", "The existing Coordica launcher matches. Other client options were retained; check that the service is enabled and trusted in the client."
		return result
	}
	if !options.Apply {
		result.Status, result.Message = "preview", "Will merge mcpServers."+serverName(options)+" and preserve other settings; an existing file will be backed up."
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
	name := serverName(options) + ".mcp.json"
	content, _, _ := mergeNamedJSON(nil, launcher(options, false), serverName(options))
	message := "Import this JSON using the client's MCP configuration interface."
	if client == "dsh" {
		name = serverName(options) + ".cordis.yml"
		quote := func(s string) string { raw, _ := json.Marshal(s); return string(raw) }
		var args strings.Builder
		for _, arg := range arguments(options) {
			fmt.Fprintf(&args, "          - %s\n", quote(arg))
		}
		id := "coordica-bootstrap"
		if options.Mode == "chat" {
			id = "coordica-chat"
		}
		content = []byte("- insert:\n    - id: " + id + "\n      name: '@deepseek-ai/dsh-mcp-client'\n      config:\n        serverName: " + serverName(options) + "\n        transport: stdio\n        command: " + quote(options.Executable) + "\n        args:\n" + args.String() + "        env: {}\n        toolCallTimeoutMs: 180000\n")
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
