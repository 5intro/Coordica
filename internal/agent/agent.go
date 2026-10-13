// Package agent adapts documented programmatic interfaces. A successful run
// is a tool result, never an approval to integrate its code.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrProtocol = errors.New("invalid or incomplete agent protocol")

type Request struct {
	Tool, Executable, WorkDir, Prompt, ResumeSession, Role string
	OutputSchemaPath                                       string
	AllowedTools                                           []string
}
type Event struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}
type Result struct {
	SessionID   string `json:"session_id"`
	OutputText  string `json:"output_text"`
	ExitCode    int    `json:"exit_code"`
	TreeStopped bool   `json:"tree_stopped"`
}
type Capability struct {
	Tool       string `json:"tool"`
	Version    string `json:"version"`
	Executable string `json:"executable,omitempty"`
	Protocol   string `json:"protocol"`
	Available  bool   `json:"available"`
	Structured bool   `json:"structured"`
	Resume     bool   `json:"resume"`
	Cancel     bool   `json:"cancel"`
	Review     bool   `json:"review"`
	ReadOnly   bool   `json:"read_only"`
	Verified   bool   `json:"verified"`
	Approvals  string `json:"approvals"`
	Detail     string `json:"detail"`
}

func Probe(ctx context.Context, tool, executable string) Capability {
	return DefaultRegistry().Probe(ctx, tool, executable)
}

func Execute(ctx context.Context, req Request, onEvent func(Event)) (Result, error) {
	return DefaultRegistry().Execute(ctx, req, onEvent)
}

func (r *Registry) Execute(ctx context.Context, req Request, onEvent func(Event)) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{ExitCode: -1}, err
	}
	def, found := r.Resolve(req.Tool)
	if !found {
		return Result{ExitCode: -1}, errors.New("unregistered agent tool")
	}
	req.Tool = def.ID
	if req.OutputSchemaPath != "" && def.Protocol != ProtocolCodex {
		return Result{ExitCode: -1}, errors.New("registered protocol does not support output schemas")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return Result{ExitCode: -1}, errors.New("prompt is required")
	}
	if len(req.Prompt) > 8<<20 {
		return Result{ExitCode: -1}, errors.New("agent prompt exceeds 8 MiB")
	}
	if req.WorkDir == "" {
		return Result{ExitCode: -1}, errors.New("fixed working directory is required")
	}
	if req.ResumeSession != "" && !def.Resume {
		return Result{ExitCode: -1}, errors.New("registered tool does not support resume")
	}
	if isReview(req) && !def.Review {
		return Result{ExitCode: -1}, errors.New("registered tool does not support independent review")
	}
	dir, err := filepath.Abs(req.WorkDir)
	if err != nil {
		return Result{ExitCode: -1}, err
	}
	req.WorkDir = dir
	if def.Protocol == ProtocolPi && req.ResumeSession != "" {
		if err := validatePiResume(req); err != nil {
			return Result{ExitCode: -1}, err
		}
	}
	req.Executable, err = resolveExecutable(def, req.Executable)
	if err != nil {
		return Result{ExitCode: -1}, err
	}
	if def.MinimumVersion != "" {
		capability := r.Probe(ctx, def.ID, req.Executable)
		if !capability.Available {
			if err := ctx.Err(); err != nil {
				return Result{ExitCode: -1}, err
			}
			return Result{ExitCode: -1}, errors.New(capability.Detail)
		}
	}
	if err := ctx.Err(); err != nil {
		return Result{ExitCode: -1}, err
	}
	if onEvent == nil {
		onEvent = func(Event) {}
	}
	switch def.Protocol {
	case ProtocolACP:
		return executeACP(ctx, def, req, onEvent)
	case ProtocolPi:
		return executePi(ctx, def, req, onEvent)
	default:
		return executeJSONL(ctx, def, req, onEvent)
	}
}

// Kept for old local adapter tests. Execution itself resolves registrations.
func commandArgs(req Request) []string {
	def, _ := DefaultRegistry().Resolve(req.Tool)
	return launchArgs(def, req)
}

func sanitize(raw []byte) json.RawMessage {
	return sanitizeWithSecrets(raw, knownSecrets())
}

// SanitizeEvent also removes literal known model, publication and additional
// caller-supplied secrets from string values. Arbitrary unknown secrets in a
// repository or natural-language output still require private log storage.
func SanitizeEvent(event Event, secrets ...string) Event {
	secrets = append(knownSecrets(), secrets...)
	return Event{Type: redactText(event.Type, secrets), Data: sanitizeWithSecrets(event.Data, secrets)}
}
func knownSecrets() []string {
	var values []string
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		key = strings.ToUpper(key)
		if ok && value != "" && (strings.HasSuffix(key, "_TOKEN") || strings.HasSuffix(key, "_API_KEY") || key == "COORDICA_GIT_AUTHORIZATION") {
			values = append(values, value)
		}
	}
	return values
}
func redactText(text string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[redacted]")
		}
	}
	return text
}

func sanitizeWithSecrets(raw []byte, secrets []string) json.RawMessage {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	var walk func(any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case map[string]any:
			for key, item := range x {
				lower := strings.ToLower(key)
				switch lower {
				case "authorization", "api_key", "apikey", "token", "access_token", "refresh_token", "password", "secret":
					x[key] = "[redacted]"
				default:
					x[key] = walk(item)
				}
			}
		case []any:
			for i, item := range x {
				x[i] = walk(item)
			}
		case string:
			return redactText(x, secrets)
		}
		return v
	}
	value = walk(value)
	data, _ := json.Marshal(value)
	return data
}

// Preserve official model authentication but do not inject publication or
// center authority into an agent process. This is credential hygiene in a
// private development environment, not a sandbox against an owner-level tool.
func developmentEnv() []string {
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		key = strings.ToUpper(key)
		publicationToken := (strings.HasPrefix(key, "GH_") || strings.HasPrefix(key, "GITHUB_") || strings.HasPrefix(key, "GITLAB_")) && strings.Contains(key, "TOKEN")
		if publicationToken || key == "SSH_AUTH_SOCK" || key == "SSH_AGENT_PID" || strings.HasPrefix(key, "GIT_") || strings.HasPrefix(key, "COORDICA_") {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never")
}

func emit(onEvent func(Event), kind string, data any) {
	raw, _ := json.Marshal(data)
	onEvent(Event{Type: kind, Data: sanitize(raw)})
}

func rpcError(raw json.RawMessage) error { return fmt.Errorf("ACP request failed: %s", sanitize(raw)) }

// Leader gets only CLI/platform essentials, standard proxy transport settings
// and official model authentication.
// Arbitrarily named owner/publication tokens are not forwarded to planning.
func executionEnv(req Request) []string {
	if req.Role != "leader" {
		return developmentEnv()
	}
	allowed := map[string]bool{"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "SYSTEMROOT": true, "WINDIR": true, "TMPDIR": true, "TMP": true, "TEMP": true, "LANG": true, "LC_ALL": true, "CODEX_HOME": true, "XDG_CONFIG_HOME": true, "OPENAI_API_KEY": true, "HTTPS_PROXY": true, "HTTP_PROXY": true, "ALL_PROXY": true, "NO_PROXY": true}
	env := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if allowed[strings.ToUpper(key)] {
			env = append(env, entry)
		}
	}
	return append(env, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never")
}
