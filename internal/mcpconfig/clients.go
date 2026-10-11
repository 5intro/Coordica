package mcpconfig

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

func configureVersioned(ctx context.Context, options Options, client string, deps dependencies) Result {
	if client == "kimi" {
		if deps.getenv != nil && deps.getenv("KIMI_CODE_HOME") != "" {
			base, err := clientBase(options.Home, "KIMI_CODE_HOME", deps)
			if err != nil {
				return Result{Client: client, Status: "error", Message: err.Error()}
			}
			return configureJSON(options, client, base, filepath.Join(base, "mcp.json"))
		}
		var found []string
		for _, name := range []string{".kimi-code", ".kimi"} {
			path := filepath.Join(options.Home, name)
			if err := noLinks(options.Home, path); err != nil {
				return Result{Client: client, Status: "error", Message: err.Error()}
			}
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				found = append(found, path)
			}
		}
		if len(found) != 1 {
			return Result{Client: client, Status: "unsupported", Message: "Kimi's old and new configuration layouts cannot be distinguished. Keep one active layout or set the new Kimi Code's KIMI_CODE_HOME; generic export is also available."}
		}
		return configureJSON(options, client, options.Home, filepath.Join(found[0], "mcp.json"))
	}
	executable, err := deps.lookPath("pi")
	if err != nil {
		return Result{Client: client, Status: "unsupported", Message: "pi must be installed so its native MCP support can be checked (version 0.99.0 or newer)."}
	}
	version, err := deps.command(ctx, executable, []string{"--version"}, "")
	if err != nil || !piSupportsMCP(string(version)) {
		return Result{Client: client, Status: "unsupported", Message: "Could not confirm pi 0.99.0 or newer. Upgrade pi before installing native MCP configuration."}
	}
	base, err := clientBase(filepath.Join(options.Home, ".pi", "agent"), "PI_CODING_AGENT_DIR", deps)
	if err != nil {
		return Result{Client: client, Status: "error", Message: err.Error()}
	}
	boundary := options.Home
	if deps.getenv != nil && deps.getenv("PI_CODING_AGENT_DIR") != "" {
		boundary = base
	}
	return configureJSON(options, client, boundary, filepath.Join(base, "mcp.json"))
}

var piVersion = regexp.MustCompile(`^(?:pi )?v?(\d+)\.(\d+)\.(\d+)(?:\+[a-zA-Z0-9.-]+)?$`)

func piSupportsMCP(version string) bool {
	parts := piVersion.FindStringSubmatch(strings.TrimSpace(version))
	if len(parts) != 4 {
		return false
	}
	major, errMajor := strconv.Atoi(parts[1])
	minor, errMinor := strconv.Atoi(parts[2])
	_, errPatch := strconv.Atoi(parts[3])
	return errMajor == nil && errMinor == nil && errPatch == nil && (major > 0 || minor >= 99)
}

func codexEntry(raw, expected []byte) (exists, matches, disabled bool, err error) {
	var entries []json.RawMessage
	if json.Unmarshal(raw, &entries) != nil || entries == nil {
		return false, false, false, errors.New("Codex returned an unexpected MCP JSON list; configuration was not changed")
	}
	for _, entry := range entries {
		var fields map[string]json.RawMessage
		if object(entry, &fields) != nil {
			return false, false, false, errors.New("Codex returned an ambiguous MCP JSON entry; configuration was not changed")
		}
		var name string
		if json.Unmarshal(fields["name"], &name) != nil || name == "" {
			return false, false, false, errors.New("Codex returned an unnamed MCP JSON entry; configuration was not changed")
		}
		if name != "coordica" {
			continue
		}
		if exists {
			return false, false, false, errors.New("Codex returned duplicate Coordica entries; configuration was not changed")
		}
		exists = true
		matches = sameLauncher(fields["transport"], expected)
		var enabled *bool
		if json.Unmarshal(fields["enabled"], &enabled) != nil || enabled == nil {
			return false, false, false, errors.New("Codex returned invalid enabled state; configuration was not changed")
		}
		disabled = !*enabled
	}
	return exists, matches, disabled, nil
}

func configureCodex(ctx context.Context, options Options, deps dependencies) Result {
	result := Result{Client: "codex", Content: string(launcher(options, true))}
	executable, err := deps.lookPath("codex")
	if err != nil {
		result.Status, result.Message = "unsupported", "Install the Codex CLI to register this MCP with its official configuration commands."
		return result
	}
	base, err := clientBase(filepath.Join(options.Home, ".codex"), "CODEX_HOME", deps)
	if err != nil {
		result.Status, result.Message = "error", err.Error()
		return result
	}
	boundary := options.Home
	if deps.getenv != nil && deps.getenv("CODEX_HOME") != "" {
		boundary = base
	}
	result.Path = filepath.Join(base, "config.toml")
	// Codex list loads project configuration from its process directory, while
	// add writes user configuration. Keep every query and mutation in the same
	// empty temporary directory so the invoking project's entry cannot stand in
	// for a user-level registration. This is a config scope, not a sandbox.
	neutral, err := os.MkdirTemp("", "coordica-mcp-setup-")
	if err != nil {
		result.Status, result.Message = "error", "A neutral Codex configuration directory cannot be created."
		return result
	}
	defer os.RemoveAll(neutral)
	neutral, err = filepath.EvalSymlinks(neutral)
	if err != nil {
		result.Status, result.Message = "error", "The neutral Codex directory cannot be resolved."
		return result
	}
	for parent := filepath.Dir(neutral); ; parent = filepath.Dir(parent) {
		if _, err := os.Lstat(filepath.Join(parent, ".git")); err == nil {
			result.Status, result.Message = "error", "The temporary directory is inside a Git project. Select a system temporary directory before configuring Codex."
			return result
		}
		if filepath.Dir(parent) == parent {
			break
		}
	}
	query := func() (exists, matches, disabled bool, err error) {
		raw, err := deps.command(ctx, executable, []string{"mcp", "list", "--json"}, neutral)
		if err != nil {
			return false, false, false, errors.New("Codex MCP query failed; no registration was attempted")
		}
		return codexEntry(raw, launcher(options, true))
	}
	existing := func(matches, disabled bool) Result {
		if !matches {
			result.Status, result.Message = "conflict", "A different Coordica MCP entry already exists in Codex; left unchanged."
		} else {
			result.Status, result.Message = "unchanged", "The existing Codex launcher matches. Its timeout, environment and other options were retained."
			if disabled {
				result.Message += " The service is disabled; enable it in Codex before using its tools."
			} else {
				result.Message += " Reload Codex and confirm its MCP tools."
			}
		}
		return result
	}
	before, err := readSnapshot(boundary, result.Path)
	if err != nil {
		result.Status, result.Message = "error", err.Error()
		return result
	}
	exists, matches, disabled, err := query()
	if err != nil {
		result.Status, result.Message = "error", err.Error()
		return result
	}
	if exists {
		return existing(matches, disabled)
	}
	if !options.Apply {
		result.Status, result.Message = "preview", "Will register with the official codex mcp add command, then verify its structured configuration."
		return result
	}
	if err := os.MkdirAll(base, 0700); err != nil {
		result.Status, result.Message = "error", "Codex configuration directory cannot be created."
		return result
	}
	lock := result.Path + ".coordica-setup.lock"
	if err := os.Mkdir(lock, 0700); err != nil {
		result.Status, result.Message = "error", "Another setup operation owns the Codex configuration."
		return result
	}
	defer os.Remove(lock)
	// add replaces a same-name server in Codex. Re-query under our own writer
	// lock and compare the file again before ever issuing that command.
	exists, matches, disabled, err = query()
	if err != nil {
		result.Status, result.Message = "error", err.Error()
		return result
	}
	if exists {
		return existing(matches, disabled)
	}
	current, err := readSnapshot(boundary, result.Path)
	if err != nil || !sameSnapshot(before, current) {
		result.Status, result.Message = "error", errChanged.Error()
		return result
	}
	backup := ""
	if before.info != nil {
		backup, err = privateTemp(base, ".config.toml.coordica-backup-*", before.data)
		if err != nil {
			result.Status, result.Message = "error", err.Error()
			return result
		}
	}
	args := append([]string{"mcp", "add", "coordica", "--", options.Executable}, arguments(options)...)
	if _, err := deps.command(ctx, executable, args, neutral); err != nil {
		result.Status, result.Message = "error", "Codex registration command failed. Its result is unconfirmed; inspect Codex and retry setup."
		return result
	}
	exists, matches, disabled, err = query()
	if err != nil || !exists || !matches || disabled {
		result.Status, result.Message = "error", "Codex registration could not be verified as enabled; inspect the client configuration before retrying."
		return result
	}
	result.Status, result.Message = "installed", "The official Codex CLI registered and verified the MCP launcher. Reload Codex and confirm its tools."
	if backup != "" {
		result.Message += " Previous configuration backup: " + backup
	}
	return result
}
