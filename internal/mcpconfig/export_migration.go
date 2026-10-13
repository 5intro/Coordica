package mcpconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func exportContent(o Options, client string) []byte {
	content, _, _ := mergeNamedJSON(nil, launcher(o, false), serverName(o))
	if client != "dsh" {
		return content
	}
	quote := func(s string) string { raw, _ := json.Marshal(s); return string(raw) }
	var args strings.Builder
	for _, arg := range arguments(o) {
		fmt.Fprintf(&args, "          - %s\n", quote(arg))
	}
	id := "coordica-bootstrap"
	if o.Mode == "chat" {
		id = "coordica-chat"
	} else if o.Mode == "interaction" || o.Mode == "" {
		id = "coordica"
	}
	return []byte("- insert:\n    - id: " + id + "\n      name: '@deepseek-ai/dsh-mcp-client'\n      config:\n        serverName: " + serverName(o) + "\n        transport: stdio\n        command: " + quote(o.Executable) + "\n        args:\n" + args.String() + "        env: {}\n        toolCallTimeoutMs: 180000\n")
}

// Only byte-for-byte outputs of this installer are managed export artifacts.
// Loaded dsh overlays and arbitrary client profiles remain outside its control.
func ownedExport(raw []byte, o Options, client, name string) (Options, json.RawMessage, error) {
	var entry json.RawMessage
	if client == "generic" {
		var root, servers map[string]json.RawMessage
		if object(raw, &root) != nil || len(root) != 1 || object(root["mcpServers"], &servers) != nil || len(servers) != 1 || servers[name] == nil {
			return o, nil, errors.New("export is not an owned installer artifact; left unchanged")
		}
		entry = servers[name]
	} else {
		var command string
		var args []string
		collecting := false
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(line, "        command: ") {
				if json.Unmarshal([]byte(strings.TrimPrefix(line, "        command: ")), &command) != nil {
					return o, nil, errors.New("unowned dsh export")
				}
			}
			if line == "        args:" {
				collecting = true
				continue
			}
			if collecting && strings.HasPrefix(line, "          - ") {
				var arg string
				if json.Unmarshal([]byte(strings.TrimPrefix(line, "          - ")), &arg) != nil {
					return o, nil, errors.New("unowned dsh export")
				}
				args = append(args, arg)
			} else if collecting {
				collecting = false
			}
		}
		entry, _ = json.Marshal(map[string]any{"command": command, "args": args})
	}
	old, err := ownedLauncher(entry, o)
	if err != nil || serverName(old) != name || string(exportContent(old, client)) != string(raw) {
		return o, nil, errors.New("different or unowned exported configuration; left unchanged")
	}
	return old, entry, nil
}
func removeOwnedExport(base, path string, before snapshot) error {
	lock := path + ".coordica-setup.lock"
	if err := os.Mkdir(lock, 0700); err != nil {
		return errors.New("old export has another writer; retry setup")
	}
	defer os.Remove(lock)
	current, err := readSnapshot(base, path)
	if err != nil || !sameSnapshot(before, current) {
		return errChanged
	}
	if _, err = privateTemp(filepath.Dir(path), "."+filepath.Base(path)+".coordica-backup-*", before.data); err != nil {
		return err
	}
	current, err = readSnapshot(base, path)
	if err != nil || !sameSnapshot(before, current) {
		return errChanged
	}
	if err = os.Remove(path); err != nil {
		return errors.New("owned old export could not be removed; retry setup")
	}
	return nil
}
func exportInteraction(o Options, client string) Result {
	extension := ".mcp.json"
	message := "Import the single coordica JSON configuration."
	if client == "dsh" {
		extension = ".cordis.yml"
		message = "Load this one overlay with dsh web --patch and its quoted absolute path. Remove previously loaded coordica-bootstrap/coordica-chat overlays from your own dsh launch command; setup does not modify loaded overlays or arbitrary dsh profiles."
	}
	path := filepath.Join(o.OutputDir, "coordica"+extension)
	oldPath := filepath.Join(o.OutputDir, "coordica-chat"+extension)
	result := Result{Client: client, Path: path}
	base := o.ConfigHome
	if rel, err := filepath.Rel(base, o.OutputDir); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		base = o.OutputDir
	}
	before, err := readSnapshot(base, path)
	if err != nil {
		result.Status, result.Message = "error", err.Error()
		return result
	}
	old, err := readSnapshot(base, oldPath)
	if err != nil {
		result.Status, result.Message = "error", err.Error()
		return result
	}
	servers := map[string]json.RawMessage{}
	for _, item := range []struct {
		name  string
		state snapshot
	}{{"coordica", before}, {"coordica-chat", old}} {
		if item.state.info == nil {
			continue
		}
		_, entry, err := ownedExport(item.state.data, o, client, item.name)
		if err != nil {
			result.Status, result.Message = "conflict", err.Error()
			return result
		}
		servers[item.name] = entry
	}
	hadLocal, hadChat := servers["coordica"] != nil, servers["coordica-chat"] != nil
	wasInteraction := isInteractionLauncher(servers["coordica"], o)
	selected, _, err := mergeInteraction(servers, o, false)
	if err != nil {
		result.Status, result.Message = "conflict", err.Error()
		return result
	}
	content := exportContent(selected, client)
	result.Content = string(content)
	if hadLocal {
		result.existingRunner = selected.RunnerData
	}
	if hadChat || wasInteraction {
		result.existingChat = selected.ChatProfile
	}
	same := before.info != nil && string(before.data) == string(content)
	if same && old.info == nil {
		result.Status, result.Message = "unchanged", message
		return result
	}
	if !o.Apply {
		result.Status, result.Message = "preview", "Will export one current coordica configuration, replace only an owned matching legacy export, and privately back up/remove the owned matching coordica-chat export if present. "+message
		return result
	}
	current, err := readSnapshot(base, oldPath)
	if err != nil || !sameSnapshot(old, current) {
		result.Status, result.Message = "error", errChanged.Error()
		return result
	}
	if !same {
		if _, err = writeConfig(base, path, before, content); err != nil {
			result.Status, result.Message = "error", err.Error()
			return result
		}
	}
	if old.info != nil {
		if err = removeOwnedExport(base, oldPath, old); err != nil {
			result.Status, result.Message = "error", "Current coordica export saved; old export retained or removal unconfirmed. Retry setup: "+err.Error()
			return result
		}
	}
	result.Status, result.Message = "installed", message
	return result
}
