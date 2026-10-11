package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const ExecutionIntentVersion = 1

// ExecutionIntent is the v1 native final-output contract. It states intent,
// never completion evidence or permission to change repository paths.
type ExecutionIntent struct {
	Version        int      `json:"version"`
	Status         string   `json:"status"`
	RequestedPaths []string `json:"requested_paths"`
	Reason         string   `json:"reason"`
	Summary        string   `json:"summary"`
}

func ParseExecutionIntent(text string) (ExecutionIntent, error) {
	var out ExecutionIntent
	invalid := func() (ExecutionIntent, error) {
		return out, fmt.Errorf("invalid execution intent v%d", ExecutionIntentVersion)
	}
	if len(text) > 64<<10 || !utf8.ValidString(text) {
		return invalid()
	}
	d := json.NewDecoder(strings.NewReader(text))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return invalid()
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return invalid()
		}
		name, ok := key.(string)
		if !ok || fields[name] != nil {
			return invalid()
		}
		switch name {
		case "version", "status", "requested_paths", "reason", "summary":
		default:
			return invalid()
		}
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return invalid()
		}
		fields[name] = value
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') || len(fields) != 5 {
		return invalid()
	}
	if d.Decode(new(any)) != io.EOF || json.Unmarshal(fields["version"], &out.Version) != nil || json.Unmarshal(fields["status"], &out.Status) != nil || json.Unmarshal(fields["reason"], &out.Reason) != nil || json.Unmarshal(fields["summary"], &out.Summary) != nil {
		return invalid()
	}
	var paths []json.RawMessage
	if json.Unmarshal(fields["requested_paths"], &paths) != nil || len(paths) > 100 {
		return invalid()
	}
	out.RequestedPaths = make([]string, len(paths))
	seen := map[string]bool{}
	for i, raw := range paths {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &out.RequestedPaths[i]) != nil {
			return invalid()
		}
		p := out.RequestedPaths[i]
		if strings.TrimSpace(p) == "" || len(p) > 1024 || strings.ContainsRune(p, 0) || seen[p] {
			return invalid()
		}
		seen[p] = true
	}
	if out.Version != ExecutionIntentVersion || len(out.Reason) > 4096 || len(out.Summary) > 4096 || strings.TrimSpace(out.Summary) == "" || strings.ContainsRune(out.Reason+out.Summary, 0) {
		return invalid()
	}
	switch out.Status {
	case "completed":
		if len(paths) != 0 || out.Reason != "" {
			return invalid()
		}
	case "scope_requested":
		if len(paths) == 0 || strings.TrimSpace(out.Reason) == "" {
			return invalid()
		}
	case "blocked":
		if len(paths) != 0 || strings.TrimSpace(out.Reason) == "" {
			return invalid()
		}
	default:
		return invalid()
	}
	return out, nil
}
