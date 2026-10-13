package mcpconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
)

func interactionDefaults(o Options) Options {
	if o.RunnerData == "" {
		o.RunnerData = filepath.Join(o.ConfigHome, "Coordica", "runner")
	}
	if o.ChatProfile == "" {
		o.ChatProfile = filepath.Join(o.ConfigHome, "Coordica", "interaction.json")
	}
	return o
}

// Only exact, closed launcher forms emitted by this installer establish
// ownership. Names, generic commands, endpoints and unknown flags do not.
func ownedLauncher(raw []byte, o Options) (Options, error) {
	var fields map[string]json.RawMessage
	if object(raw, &fields) != nil {
		return o, errors.New("ambiguous Coordica entry; left unchanged")
	}
	var command string
	var args []string
	if json.Unmarshal(fields["command"], &command) != nil || command != o.Executable || json.Unmarshal(fields["args"], &args) != nil || len(args) < 2 || args[0] != "mcp" {
		return o, errors.New("Coordica launcher ownership differs; left unchanged")
	}
	probe, _ := json.Marshal(map[string]any{"command": command, "args": args})
	if !sameLauncher(raw, probe) {
		return o, errors.New("Coordica transport differs; left unchanged")
	}
	selected := o
	selected.RunnerData, selected.ChatProfile, selected.ToolsFile, selected.HostRole, selected.HostConfig = "", "", "", "", ""
	switch args[1] {
	case "--bootstrap":
		selected.Mode = "bootstrap"
	case "--interaction":
		selected.Mode = "interaction"
	case "--chat-profile":
		selected.Mode = "chat"
		args = append([]string{"mcp", "chat"}, args[1:]...)
	default:
		return o, errors.New("unknown Coordica launcher identity; left unchanged")
	}
	seen := map[string]bool{}
	for i := 2; i < len(args); i += 2 {
		if i+1 == len(args) || seen[args[i]] || args[i] != "--host-role" && !filepath.IsAbs(args[i+1]) || strings.ContainsAny(args[i+1], "\x00\r\n") {
			return o, errors.New("unsafe or ambiguous Coordica paths; left unchanged")
		}
		seen[args[i]] = true
		switch args[i] {
		case "--host-role":
			if args[i+1] != "center" && args[i+1] != "node" {
				return o, errors.New("invalid owned host role; left unchanged")
			}
			selected.HostRole = args[i+1]
		case "--host-config":
			selected.HostConfig = args[i+1]
		case "--runner-data":
			selected.RunnerData = args[i+1]
		case "--chat-profile":
			selected.ChatProfile = args[i+1]
		case "--tools-file":
			selected.ToolsFile = args[i+1]
		default:
			return o, errors.New("unowned Coordica launcher options; left unchanged")
		}
	}
	if selected.HostRole == "center" && (selected.Mode != "interaction" || selected.HostConfig == "") || selected.HostRole != "center" && selected.HostConfig != "" {
		return o, errors.New("ambiguous host role/configuration; left unchanged")
	}
	if selected.Mode == "chat" && (selected.ChatProfile == "" || selected.RunnerData != "" || selected.ToolsFile != "") || selected.Mode == "bootstrap" && selected.ChatProfile != "" || selected.Mode == "interaction" && (selected.HostRole != "center" && selected.RunnerData == "" || selected.ChatProfile == "") {
		return o, errors.New("Coordica identity options differ; left unchanged")
	}
	if selected.Mode == "bootstrap" && selected.RunnerData == "" {
		base, err := legacyConfigHome(fields["env"], o)
		if err != nil {
			return o, err
		}
		selected.RunnerData = filepath.Join(base, "Coordica", "runner")
	}
	return selected, nil
}

// os.UserConfigDir uses HOME on macOS, APPDATA on Windows, and
// XDG_CONFIG_HOME (falling back to HOME/.config) on Unix. Only these launcher
// overrides affect the implicit bootstrap identity; CODEX_HOME is unrelated.
func legacyConfigHome(raw json.RawMessage, o Options) (string, error) {
	var env map[string]string
	if len(raw) > 0 && json.Unmarshal(raw, &env) != nil {
		return "", errors.New("ambiguous legacy launcher environment; implicit Runner path left unchanged")
	}
	base := o.ConfigHome
	switch runtime.GOOS {
	case "darwin", "ios":
		if home, ok := env["HOME"]; ok {
			if home == "" {
				base = ""
			} else {
				base = filepath.Join(home, "Library", "Application Support")
			}
		}
	case "windows":
		found := false
		for key, appdata := range env {
			if strings.EqualFold(key, "APPDATA") {
				if found && base != appdata {
					return "", errors.New("ambiguous legacy APPDATA overrides; Runner identity left unchanged")
				}
				base, found = appdata, true
			}
		}
	case "plan9":
		if _, ok := env["home"]; ok {
			return "", errors.New("legacy Plan 9 implicit Runner identity requires explicit migration")
		}
	default:
		if xdg, ok := env["XDG_CONFIG_HOME"]; ok && xdg != "" {
			base = xdg
		} else if home, ok := env["HOME"]; ok {
			// Without an explicit XDG override, the earlier inherited value
			// cannot be recovered from today's installer environment.
			if _, explicit := env["XDG_CONFIG_HOME"]; !explicit || home == "" {
				return "", errors.New("legacy implicit Runner environment requires explicit migration; left unchanged")
			}
			base = filepath.Join(home, ".config")
		} else if xdg, ok := env["XDG_CONFIG_HOME"]; ok && xdg == "" {
			return "", errors.New("legacy empty XDG override requires an explicit HOME to establish Runner identity; left unchanged")
		}
	}
	if !filepath.IsAbs(base) || strings.ContainsAny(base, "\x00\r\n") {
		return "", errors.New("unsafe legacy implicit Runner directory; left unchanged")
	}
	return base, nil
}

func preservePath(selected *string, old string) error {
	if *selected != "" && *selected != old {
		return errors.New("selected Coordica paths conflict with existing identity; left unchanged")
	}
	*selected = old
	return nil
}

// mergeInteraction is shared by JSON and the Codex TOML adapter. All ownership,
// path and option conflicts are checked before the resulting plan is written.
func mergeInteraction(servers map[string]json.RawMessage, o Options, withType bool) (Options, bool, error) {
	have, haveMain := servers["coordica"]
	chat, haveChat := servers["coordica-chat"]
	mainFields := map[string]json.RawMessage{}
	if haveMain {
		old, err := ownedLauncher(have, o)
		if err != nil || old.Mode == "chat" {
			return o, false, errors.New("coordica is not an owned local/interaction launcher; left unchanged")
		}
		if o.HostRole != "" && (old.HostRole != "" && o.HostRole != old.HostRole || old.HostRole == "" && o.HostRole == "center") {
			return o, false, errors.New("selected host role conflicts with owned launcher; choose a separate client/export registration explicitly")
		}
		if old.HostRole != "" {
			if err = preservePath(&o.HostRole, old.HostRole); err != nil {
				return o, false, err
			}
		}
		if err = preservePath(&o.HostConfig, old.HostConfig); err != nil {
			return o, false, err
		}
		old = interactionDefaults(old)
		if err = preservePath(&o.RunnerData, old.RunnerData); err != nil {
			return o, false, err
		}
		if old.Mode == "interaction" {
			if err = preservePath(&o.ChatProfile, old.ChatProfile); err != nil {
				return o, false, err
			}
		}
		if err = preservePath(&o.ToolsFile, old.ToolsFile); err != nil {
			return o, false, err
		}
		_ = object(have, &mainFields)
	}
	if haveChat {
		old, err := ownedLauncher(chat, o)
		if err != nil || old.Mode != "chat" {
			return o, false, errors.New("coordica-chat is not an owned limited launcher; left unchanged")
		}
		if err = preservePath(&o.ChatProfile, old.ChatProfile); err != nil {
			return o, false, err
		}
		var fields map[string]json.RawMessage
		_ = object(chat, &fields)
		for k, v := range fields {
			if k == "command" || k == "args" || k == "type" || k == "transport" {
				continue
			}
			if existing, ok := mainFields[k]; ok {
				var a, b any
				_ = json.Unmarshal(existing, &a)
				_ = json.Unmarshal(v, &b)
				if !reflect.DeepEqual(a, b) {
					return o, false, errors.New("legacy Coordica client options conflict; left unchanged")
				}
			} else {
				mainFields[k] = v
			}
		}
	}
	o = interactionDefaults(o)
	var expected map[string]json.RawMessage
	_ = object(launcher(o, withType), &expected)
	for k, v := range expected {
		mainFields[k] = v
	}
	if haveMain && !haveChat && sameLauncher(have, launcher(o, withType)) {
		return o, true, nil
	}
	servers["coordica"], _ = marshalClientSettings(mainFields)
	delete(servers, "coordica-chat")
	return o, false, nil
}
func mergeInteractionJSON(raw []byte, o Options, withType bool) ([]byte, Options, bool, error) {
	root := map[string]json.RawMessage{}
	if len(raw) > 0 && object(raw, &root) != nil {
		return nil, o, false, errors.New("existing configuration is not an unambiguous JSON object; left unchanged")
	}
	servers := map[string]json.RawMessage{}
	if value, ok := root["mcpServers"]; ok && object(value, &servers) != nil {
		return nil, o, false, errors.New("mcpServers is not a JSON object; left unchanged")
	}
	_, hadChat := servers["coordica-chat"]
	selected, same, err := mergeInteraction(servers, o, withType)
	if err != nil || same {
		return raw, selected, same, err
	}
	// Retain disabled/trust decisions that reference the retired owned name.
	if hadChat {
		if _, err := renameClientReferences(root); err != nil {
			return nil, o, false, err
		}
		// Claude stores per-project enable/disable decisions independently of
		// its user launcher. Keep those decisions attached to the merged name.
		var projects map[string]json.RawMessage
		if value, exists := root["projects"]; exists {
			if object(value, &projects) != nil {
				return nil, o, false, errors.New("ambiguous project MCP state; left unchanged")
			}
			projectsChanged := false
			for key, raw := range projects {
				var project map[string]json.RawMessage
				if object(raw, &project) != nil {
					return nil, o, false, errors.New("ambiguous project MCP state; left unchanged")
				}
				changed, err := renameClientReferences(project)
				if err != nil {
					return nil, o, false, err
				}
				if changed {
					projects[key], _ = marshalClientSettings(project)
					projectsChanged = true
				}
			}
			if projectsChanged {
				root["projects"], _ = marshalClientSettings(projects)
			}
		}
	}
	root["mcpServers"], _ = marshalClientSettings(servers)
	merged, err := marshalClientSettings(root)
	if err != nil {
		return nil, o, false, err
	}
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, merged, "", "  "); err != nil {
		return nil, o, false, err
	}
	formatted.WriteByte('\n')
	return formatted.Bytes(), selected, bytes.Equal(raw, formatted.Bytes()), nil
}

// RawMessage retains unknown values (including large numbers and string
// escapes); avoid introducing HTML escapes into unrelated client settings.
func marshalClientSettings(value any) ([]byte, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(value)
	return bytes.TrimSuffix(out.Bytes(), []byte("\n")), err
}

const interactionPlan = "Will replace owned legacy Coordica launcher(s) with one coordica interaction entry and remove owned coordica-chat only when executable and selected paths match; preserve client options, disabled/trust state and unrelated entries; back up existing configuration."

func isInteractionLauncher(raw []byte, o Options) bool {
	identity, err := ownedLauncher(raw, o)
	return err == nil && identity.Mode == "interaction"
}

func renameClientReferences(fields map[string]json.RawMessage) (bool, error) {
	anyChanged := false
	disabled := false
	for _, key := range []string{"disabledMcpServers", "disabledMcpjsonServers"} {
		if value, ok := fields[key]; ok {
			var names []string
			if json.Unmarshal(value, &names) != nil || names == nil {
				return false, errors.New("ambiguous client MCP state; left unchanged")
			}
			for _, name := range names {
				disabled = disabled || name == "coordica" || name == "coordica-chat"
			}
		}
	}
	for _, key := range []string{"disabledMcpServers", "enabledMcpjsonServers", "disabledMcpjsonServers"} {
		value, ok := fields[key]
		if !ok {
			continue
		}
		var names []string
		if json.Unmarshal(value, &names) != nil || names == nil {
			return false, errors.New("ambiguous client MCP state; left unchanged")
		}
		changed := false
		merged := make([]string, 0, len(names))
		seen := false
		for _, name := range names {
			if name == "coordica-chat" {
				name = "coordica"
				changed = true
			}
			if name == "coordica" {
				if seen || key == "enabledMcpjsonServers" && disabled {
					changed = true
					continue
				}
				seen = true
			}
			merged = append(merged, name)
		}
		if changed {
			fields[key], _ = json.Marshal(merged)
			anyChanged = true
		}
	}
	return anyChanged, nil
}
