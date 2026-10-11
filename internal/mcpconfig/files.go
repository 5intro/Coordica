package mcpconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"coordica/internal/localfile"
)

const maxConfigBytes = 2 << 20

var errChanged = errors.New("configuration changed; retry after the client finishes writing")

type snapshot struct {
	data []byte
	info os.FileInfo
}

// Only the Coordica entry is returned to callers. Existing client configuration
// can include credentials and must never appear in a setup result or error.
func mergeJSON(raw, entry []byte) ([]byte, bool, error) {
	root := map[string]json.RawMessage{}
	if len(raw) != 0 {
		if err := object(raw, &root); err != nil {
			return nil, false, errors.New("existing configuration is not an unambiguous JSON object; left unchanged")
		}
	}
	servers := map[string]json.RawMessage{}
	if value, ok := root["mcpServers"]; ok {
		if err := object(value, &servers); err != nil {
			return nil, false, errors.New("mcpServers is not a JSON object; left unchanged")
		}
	}
	if existing, ok := servers["coordica"]; ok {
		if sameLauncher(existing, entry) {
			return raw, true, nil
		}
		return nil, false, errors.New("a different coordica MCP entry already exists; left unchanged")
	}
	servers["coordica"] = entry
	root["mcpServers"], _ = json.Marshal(servers)
	merged, err := json.MarshalIndent(root, "", "  ")
	return append(merged, '\n'), false, err
}

func sameLauncher(existing, expected []byte) bool {
	var have, want map[string]json.RawMessage
	if object(existing, &have) != nil || object(expected, &want) != nil {
		return false
	}
	if _, ok := have["url"]; ok {
		return false
	}
	for _, key := range []string{"type", "transport"} {
		if raw, ok := have[key]; ok {
			var value string
			if json.Unmarshal(raw, &value) != nil || value != "stdio" {
				return false
			}
		}
	}
	for _, key := range []string{"command", "args"} {
		var a, b any
		if json.Unmarshal(have[key], &a) != nil || json.Unmarshal(want[key], &b) != nil || !reflect.DeepEqual(a, b) {
			return false
		}
	}
	return true
}

// Reject duplicate keys anywhere, including unknown configuration objects;
// decoding to a map alone would silently discard an existing value.
func object(raw []byte, into *map[string]json.RawMessage) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := uniqueJSON(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("trailing JSON")
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return err
	}
	if *into == nil {
		return errors.New("object required")
	}
	return nil
}

func uniqueJSON(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return nil
	}
	keys := map[string]bool{}
	for decoder.More() {
		if delim == '{' {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || keys[name] {
				return errors.New("duplicate JSON key")
			}
			keys[name] = true
		}
		if err := uniqueJSON(decoder); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

// Config roots are selected by the user. Resolve their existing ancestors once
// so macOS's /var and /tmp aliases work; never follow links inside that root.
func canonicalBase(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("configuration directory must be absolute")
	}
	path = filepath.Clean(path)
	var tail []string
	for {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return resolved, nil
		}
		if !errors.Is(err, os.ErrNotExist) || filepath.Dir(path) == path {
			return "", errors.New("configuration directory cannot be resolved")
		}
		tail = append(tail, filepath.Base(path))
		path = filepath.Dir(path)
	}
}

func noLinks(base, path string) error {
	relative, err := filepath.Rel(base, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return errors.New("configuration path escapes its selected directory")
	}
	current := base
	parts := append([]string{""}, strings.Split(relative, string(filepath.Separator))...)
	for _, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return errors.New("configuration path cannot be inspected")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("symbolic links in client configuration paths are not supported; left unchanged")
		}
		if current != path && !info.IsDir() {
			return errors.New("configuration parent is not a directory")
		}
	}
	return nil
}

func readSnapshot(base, path string) (snapshot, error) {
	if err := noLinks(base, path); err != nil {
		return snapshot{}, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return snapshot{}, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxConfigBytes {
		return snapshot{}, errors.New("configuration must be a readable regular file of at most 2 MiB")
	}
	f, err := os.Open(path)
	if err != nil {
		return snapshot{}, errors.New("configuration cannot be read")
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return snapshot{}, errChanged
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	if err != nil || len(raw) > maxConfigBytes {
		return snapshot{}, errors.New("configuration cannot be read within the 2 MiB limit")
	}
	return snapshot{data: raw, info: info}, nil
}

func sameSnapshot(a, b snapshot) bool {
	if a.info == nil || b.info == nil {
		return a.info == nil && b.info == nil
	}
	return os.SameFile(a.info, b.info) && a.info.ModTime().Equal(b.info.ModTime()) && bytes.Equal(a.data, b.data)
}

// A private, exclusive backup is made before replacing an existing file. The
// lock serializes Coordica writers; repeated comparison also catches writes by
// other clients. External editors do not participate in a cross-process CAS.
func writeConfig(base, path string, before snapshot, data []byte) (string, error) {
	if err := noLinks(base, path); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", errors.New("configuration directory cannot be created")
	}
	lock := path + ".coordica-setup.lock"
	if err := os.Mkdir(lock, 0700); err != nil {
		return "", errors.New("another setup operation owns this configuration; left unchanged")
	}
	defer os.Remove(lock)
	current, err := readSnapshot(base, path)
	if err != nil {
		return "", err
	}
	if !sameSnapshot(before, current) {
		return "", errChanged
	}
	staged, err := privateTemp(filepath.Dir(path), ".coordica-pending-*", data)
	if err != nil {
		return "", err
	}
	defer os.Remove(staged)
	backup := ""
	if before.info != nil {
		backup, err = privateTemp(filepath.Dir(path), "."+filepath.Base(path)+".coordica-backup-*", before.data)
		if err != nil {
			return "", err
		}
	}
	current, err = readSnapshot(base, path)
	if err != nil || !sameSnapshot(before, current) {
		return backup, errChanged
	}
	if before.info == nil {
		// Linking the complete private file creates the destination exclusively;
		// a concurrently created configuration is never overwritten.
		err = os.Link(staged, path)
	} else {
		err = localfile.Replace(staged, path)
	}
	if err != nil {
		return backup, errors.New("configuration could not be installed; any existing backup was retained")
	}
	return backup, nil
}

func privateTemp(dir, pattern string, data []byte) (string, error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", errors.New("private configuration file cannot be created")
	}
	name := f.Name()
	if err = localfile.Protect(name); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		os.Remove(name)
		return "", fmt.Errorf("private configuration file could not be saved")
	}
	return name, nil
}
