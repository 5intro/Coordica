package mcpconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Codex's official list establishes effective user-scope identity. Migration
// then edits only matching named TOML tables, preserving all unrelated bytes.
// Unsupported TOML syntax is a conflict, never a best-effort rewrite.
type tomlBlock struct{ name, suffix, header, body string }

func codexBlocks(raw []byte) ([]tomlBlock, error) {
	// Multiline strings can contain apparent table headers. Refuse this
	// syntax before identifying tables instead of editing inside their text.
	if strings.Contains(string(raw), `"""`) || strings.Contains(string(raw), "'''") {
		return nil, errors.New("Codex multiline strings require explicit migration; left unchanged")
	}
	blocks := []tomlBlock{{}}
	lines := strings.SplitAfter(string(raw), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			array := strings.HasPrefix(trimmed, "[[")
			open, close := "[", "]"
			if array {
				open, close = "[[", "]]"
			}
			end := strings.Index(trimmed, close)
			if end < 0 {
				return nil, errors.New("unsupported Codex table syntax; left unchanged")
			}
			rest := strings.TrimSpace(trimmed[end+len(close):])
			if rest != "" && !strings.HasPrefix(rest, "#") {
				return nil, errors.New("ambiguous Codex table; left unchanged")
			}
			parts, err := tomlTableParts(trimmed[len(open):end])
			if err != nil {
				return nil, err
			}
			b := tomlBlock{header: line}
			if array && len(parts) > 0 && parts[0] == "mcp_servers" && (len(parts) == 1 || parts[1] == "coordica" || parts[1] == "coordica-chat") {
				return nil, errors.New("Codex owned array tables require explicit migration; left unchanged")
			}
			if len(parts) >= 2 && parts[0] == "mcp_servers" && (parts[1] == "coordica" || parts[1] == "coordica-chat") {
				b.name = parts[1]
				if len(parts) > 2 {
					b.suffix = strings.Join(parts[2:], ".")
				}
			}
			blocks = append(blocks, b)
		} else {
			blocks[len(blocks)-1].body += line
		}
	}
	return blocks, nil
}

// Read complete values for the closed launcher keys. Strings and arrays use
// TOML basic/literal strings; comments and multiline arrays are supported.
func tomlLauncherValue(raw string) (any, error) {
	var tokens []string
	array := strings.HasPrefix(strings.TrimSpace(raw), "[")
	closed := false
	for i := 0; i < len(raw); {
		c := raw[i]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == ',' || c == '[' {
			i++
			continue
		}
		if c == '#' {
			for i < len(raw) && raw[i] != '\n' {
				i++
			}
			continue
		}
		if c == ']' {
			closed = true
			i++
			continue
		}
		if closed || c != '\'' && c != '"' {
			return nil, errors.New("unsupported TOML launcher value")
		}
		quote := c
		start := i
		i++
		for i < len(raw) {
			if raw[i] == quote {
				break
			}
			if quote == '"' && raw[i] == '\\' {
				i++
			}
			i++
		}
		if i >= len(raw) {
			return nil, errors.New("unterminated TOML launcher string")
		}
		var value string
		if quote == '\'' {
			value = raw[start+1 : i]
		} else if json.Unmarshal([]byte(raw[start:i+1]), &value) != nil {
			return nil, errors.New("unsupported TOML launcher escape")
		}
		tokens = append(tokens, value)
		i++
	}
	if array {
		if !closed {
			return nil, errors.New("unterminated TOML launcher array")
		}
		return tokens, nil
	}
	if len(tokens) != 1 {
		return nil, errors.New("ambiguous TOML launcher value")
	}
	return tokens[0], nil
}

type tomlField struct{ key, raw string }

func tomlFields(body string) ([]tomlField, error) {
	var fields []tomlField
	seen := map[string]bool{}
	lines := strings.SplitAfter(body, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			fields = append(fields, tomlField{raw: line})
			continue
		}
		pos := strings.Index(line, "=")
		if pos < 0 {
			return nil, errors.New("unsupported Codex table body; left unchanged")
		}
		key := strings.Trim(strings.TrimSpace(line[:pos]), "\"'")
		if seen[key] {
			return nil, errors.New("duplicate Codex launcher keys; left unchanged")
		}
		seen[key] = true
		value := strings.TrimSpace(line[pos+1:])
		if key == "args" && strings.HasPrefix(value, "[") {
			// A closing bracket inside a quoted path is not the array terminator.
			for {
				if _, err := tomlLauncherValue(value); err == nil {
					break
				}
				i++
				if i >= len(lines) {
					return nil, errors.New("unsupported Codex args; left unchanged")
				}
				line += lines[i]
				value += lines[i]
			}
		}
		fields = append(fields, tomlField{key: key, raw: line})
	}
	return fields, nil
}
func blockLauncher(b tomlBlock) ([]byte, error) {
	fields, err := tomlFields(b.body)
	if err != nil {
		return nil, err
	}
	values := map[string]any{}
	for _, f := range fields {
		if f.key == "command" || f.key == "args" || f.key == "url" || f.key == "type" || f.key == "transport" {
			value, err := tomlLauncherValue(f.raw[strings.Index(f.raw, "=")+1:])
			if err != nil {
				return nil, err
			}
			values[f.key] = value
		}
	}
	return json.Marshal(values)
}
func codexTransports(raw []byte) (map[string]json.RawMessage, error) {
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) != nil || list == nil {
		return nil, errors.New("invalid Codex list; left unchanged")
	}
	servers := map[string]json.RawMessage{}
	for _, item := range list {
		var fields map[string]json.RawMessage
		if object(item, &fields) != nil {
			return nil, errors.New("ambiguous Codex list; left unchanged")
		}
		var name string
		if json.Unmarshal(fields["name"], &name) != nil {
			return nil, errors.New("invalid Codex name; left unchanged")
		}
		if name != "coordica" && name != "coordica-chat" {
			continue
		}
		var enabled *bool
		if json.Unmarshal(fields["enabled"], &enabled) != nil || enabled == nil || servers[name] != nil {
			return nil, errors.New("ambiguous Codex state; left unchanged")
		}
		servers[name] = fields["transport"]
	}
	return servers, nil
}
func migrateCodexTOML(raw, list []byte, o Options) ([]byte, Options, bool, error) {
	servers, err := codexTransports(list)
	if err != nil {
		return nil, o, false, err
	}
	selected, same, err := mergeInteraction(servers, o, true)
	if err != nil || same {
		return raw, selected, same, err
	}
	original, err := codexTransports(list)
	if err != nil {
		return nil, o, false, err
	}
	blocks, err := codexBlocks(raw)
	if err != nil {
		return nil, o, false, err
	}
	main, chat := -1, -1
	for i, b := range blocks {
		if b.name == "" || b.suffix != "" {
			continue
		}
		if b.name == "coordica" {
			if main >= 0 {
				return nil, o, false, errors.New("duplicate Codex table; left unchanged")
			}
			main = i
		} else {
			if chat >= 0 {
				return nil, o, false, errors.New("duplicate Codex table; left unchanged")
			}
			chat = i
		}
		have, err := blockLauncher(b)
		if err != nil || !sameLauncher(have, original[b.name]) {
			return nil, o, false, errors.New("Codex file and effective launcher differ; left unchanged")
		}
	}
	if (main >= 0) != (original["coordica"] != nil) || (chat >= 0) != (original["coordica-chat"] != nil) {
		return nil, o, false, errors.New("Codex user tables do not match effective entries; left unchanged")
	}
	target := main
	if target < 0 {
		target = chat
	}
	if target < 0 {
		return nil, o, false, errors.New("no owned legacy Codex table")
	}
	fields, err := tomlFields(blocks[target].body)
	if err != nil {
		return nil, o, false, err
	}
	var body strings.Builder
	appendField := func(raw string) {
		if body.Len() > 0 && !strings.HasSuffix(body.String(), "\n") {
			body.WriteByte('\n')
		}
		body.WriteString(raw)
	}
	extras := map[string]string{}
	for _, f := range fields {
		if f.key != "command" && f.key != "args" {
			appendField(f.raw)
			if f.key != "" {
				extras[f.key] = f.raw
			}
		}
	}
	if main >= 0 && chat >= 0 {
		other, err := tomlFields(blocks[chat].body)
		if err != nil {
			return nil, o, false, err
		}
		for _, f := range other {
			if f.key == "command" || f.key == "args" || f.key == "" {
				continue
			}
			if old, ok := extras[f.key]; ok {
				if strings.TrimSpace(old) != strings.TrimSpace(f.raw) {
					return nil, o, false, errors.New("Codex legacy options conflict; left unchanged")
				}
			} else {
				appendField(f.raw)
				extras[f.key] = f.raw
			}
		}
	}
	command, _ := json.Marshal(selected.Executable)
	args, _ := json.Marshal(arguments(selected))
	appendField(fmt.Sprintf("command = %s\nargs = %s\n", command, args))
	blocks[target].header = "[mcp_servers.coordica]\n"
	blocks[target].body = body.String()
	var out strings.Builder
	nested := map[string]string{}
	for _, b := range blocks {
		if b.name == "coordica" && b.suffix != "" {
			nested[b.suffix] = b.body
		}
	}
	for i, b := range blocks {
		if i == chat && chat != target {
			continue
		}
		if b.name == "coordica-chat" && b.suffix != "" {
			if existing, ok := nested[b.suffix]; ok {
				if existing != b.body {
					return nil, o, false, errors.New("Codex legacy nested options conflict; left unchanged")
				}
				continue
			}
			b.header = strings.Replace(b.header, "coordica-chat", "coordica", 1)
		}
		out.WriteString(b.header)
		out.WriteString(b.body)
	}
	return []byte(out.String()), selected, false, nil
}

func configureCodexMigration(ctx context.Context, o Options, deps dependencies, executable, neutral, base, boundary string, before snapshot, list []byte, result Result) Result {
	merged, selected, same, err := migrateCodexTOML(before.data, list, o)
	if err != nil {
		result.Status, result.Message = "conflict", err.Error()
		return result
	}
	result.Content = string(launcher(selected, true))
	original, _ := codexTransports(list)
	_, _, mainDisabled, _ := codexNamedEntry(list, nil, "coordica")
	_, _, chatDisabled, _ := codexNamedEntry(list, nil, "coordica-chat")
	wantDisabled := mainDisabled || chatDisabled
	if original["coordica"] != nil {
		result.existingRunner = selected.RunnerData
	}
	if original["coordica-chat"] != nil || original["coordica"] != nil && isInteractionLauncher(original["coordica"], o) {
		result.existingChat = selected.ChatProfile
	}
	if same {
		result.Status, result.Message = "unchanged", "The unified Codex launcher matches; existing disabled state and options were retained."
		return result
	}
	if !o.Apply {
		result.Status, result.Message = "preview", interactionPlan
		return result
	}
	backup, err := writeConfig(boundary, result.Path, before, merged)
	if err != nil {
		result.Status, result.Message = "error", err.Error()
		return result
	}
	// Official rediscovery validates the resulting launcher without re-adding it
	// (add would discard options such as enabled=false).
	current, err := deps.command(ctx, executable, []string{"mcp", "list", "--json"}, neutral)
	if err == nil {
		entries, e := codexTransports(current)
		exists, matches, disabled, stateErr := codexNamedEntry(current, launcher(selected, true), "coordica")
		if e != nil || stateErr != nil || entries["coordica-chat"] != nil || !exists || !matches || disabled != wantDisabled {
			err = errors.New("Codex migration could not be verified; inspect configuration before retrying")
		}
	}
	if err != nil {
		result.Status, result.Message = "error", "Codex migration written with backup but rediscovery failed; inspect configuration before retrying."
		return result
	}
	result.Status, result.Message = "installed", "One coordica interaction launcher registered; all client options retained. Backup: "+backup
	if wantDisabled {
		result.Message += " The service remains disabled in Codex."
	}
	return result
}

// Quoted dots belong to a key, not to a nested table or another server.
func tomlTableParts(raw string) ([]string, error) {
	var parts []string
	for len(strings.TrimSpace(raw)) > 0 {
		raw = strings.TrimSpace(raw)
		var part string
		end := 0
		if raw[0] == '\'' || raw[0] == '"' {
			quote := raw[0]
			end = 1
			for end < len(raw) && raw[end] != quote {
				if quote == '"' && raw[end] == '\\' {
					end++
				}
				end++
			}
			if end >= len(raw) {
				return nil, errors.New("ambiguous Codex table key; left unchanged")
			}
			if quote == '\'' {
				part = raw[1:end]
			} else if json.Unmarshal([]byte(raw[:end+1]), &part) != nil {
				return nil, errors.New("unsupported Codex table key; left unchanged")
			}
			end++
		} else {
			for end < len(raw) && (raw[end] >= 'a' && raw[end] <= 'z' || raw[end] >= 'A' && raw[end] <= 'Z' || raw[end] >= '0' && raw[end] <= '9' || raw[end] == '_' || raw[end] == '-') {
				end++
			}
			if end == 0 {
				return nil, errors.New("unsupported Codex table key; left unchanged")
			}
			part = raw[:end]
		}
		parts = append(parts, part)
		raw = strings.TrimSpace(raw[end:])
		if raw == "" {
			break
		}
		if raw[0] != '.' || strings.TrimSpace(raw[1:]) == "" {
			return nil, errors.New("ambiguous Codex table key; left unchanged")
		}
		raw = raw[1:]
	}
	return parts, nil
}
