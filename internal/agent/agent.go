// Package agent adapts documented programmatic interfaces. A successful run
// is a tool result, never an approval to integrate its code.
package agent

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

var ErrProtocol = errors.New("invalid or incomplete agent protocol")

type Request struct {
	Tool, Executable, WorkDir, Prompt, ResumeSession, Role string
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
	Available  bool   `json:"available"`
	Structured bool   `json:"structured"`
	Resume     bool   `json:"resume"`
	Cancel     bool   `json:"cancel"`
	Verified   bool   `json:"verified"`
	Approvals  string `json:"approvals"`
	Detail     string `json:"detail"`
}

func toolName(tool string) string {
	switch strings.ToLower(tool) {
	case "claude-code", "claude":
		return "claude"
	case "deepseek-harness", "dsh":
		return "dsh"
	case "codex":
		return "codex"
	}
	return ""
}
func executableFor(tool, executable string) string {
	if executable != "" {
		return executable
	}
	return tool
}

// Probe invokes only --version. Structured/Resume describe the adapter's
// documented interface, not certification of this particular tool version.
func Probe(ctx context.Context, tool, executable string) Capability {
	tool = toolName(tool)
	c := Capability{Tool: tool}
	if tool == "" {
		c.Detail = "unsupported tool"
		return c
	}
	executable = executableFor(tool, executable)
	path, err := exec.LookPath(executable)
	if err != nil {
		c.Detail = "executable unavailable"
		return c
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, result, err := process.Collect(probeCtx, process.Spec{Executable: path, Args: []string{"--version"}, Env: developmentEnv()}, 16<<10)
	if errors.Is(err, process.ErrUnsafeBatchCommand) {
		c.Detail = "Windows batch installation path is unsupported; use a path without shell metacharacters or a native .exe"
		return c
	}
	if err != nil || result.ExitCode != 0 {
		c.Detail = "version preflight failed"
		return c
	}
	c.Version = strings.TrimSpace(string(out))
	if c.Version == "" {
		c.Detail = "version preflight returned no version"
		return c
	}
	c.Available, c.Structured, c.Resume, c.Cancel = true, true, true, true
	c.Detail = "version detected; documented interface; OS/tool compatibility and authentication not certified"
	switch tool {
	case "codex":
		c.Approvals = "no interactive approval; sandbox-limited execution"
	case "claude":
		c.Approvals = "dontAsk with explicit tool allow rules; native Windows has no built-in sandbox"
	case "dsh":
		c.Approvals = "ACP requests without verifiable operation detail are rejected"
	}
	return c
}

func Execute(ctx context.Context, req Request, onEvent func(Event)) (Result, error) {
	req.Tool = toolName(req.Tool)
	if req.Tool == "" {
		return Result{ExitCode: -1}, errors.New("unsupported agent tool")
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
	dir, err := filepath.Abs(req.WorkDir)
	if err != nil {
		return Result{ExitCode: -1}, err
	}
	req.WorkDir = dir
	req.Executable = executableFor(req.Tool, req.Executable)
	if onEvent == nil {
		onEvent = func(Event) {}
	}
	if req.Tool == "dsh" {
		return executeACP(ctx, req, onEvent)
	}
	args := commandArgs(req)
	s, err := process.Start(ctx, process.Spec{Executable: req.Executable, Args: args, Dir: req.WorkDir, Env: developmentEnv()})
	if err != nil {
		return Result{ExitCode: -1}, err
	}
	emit(onEvent, "process.started", map[string]int{"pid": s.PID()})
	// Prompts use documented stdin interfaces, avoiding Windows command-line
	// limits and exposing neither task text nor diffs in process arguments.
	inputDone := make(chan error, 1)
	go func() {
		err := s.Write([]byte(req.Prompt))
		closeErr := s.CloseInput()
		inputDone <- errors.Join(err, closeErr)
	}()
	result := Result{ExitCode: -1}
	complete := false
	var protocolErr error
	for output := range s.Output() {
		if output.Stream != "stdout" {
			continue
		} // stderr is not a result protocol.
		if len(strings.TrimSpace(string(output.Line))) == 0 {
			continue
		}
		var envelope struct {
			Type      string `json:"type"`
			ThreadID  string `json:"thread_id"`
			SessionID string `json:"session_id"`
			Subtype   string `json:"subtype"`
			IsError   bool   `json:"is_error"`
			Result    string `json:"result"`
			Item      struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
		}
		if err := json.Unmarshal(output.Line, &envelope); err != nil || envelope.Type == "" {
			protocolErr = ErrProtocol
			s.Stop()
			continue
		}
		onEvent(Event{Type: envelope.Type, Data: sanitize(output.Line)})
		if req.Tool == "codex" {
			switch envelope.Type {
			case "thread.started":
				result.SessionID = envelope.ThreadID
			case "item.completed":
				if envelope.Item.Type == "agent_message" {
					result.OutputText = envelope.Item.Text
				}
			case "turn.completed":
				complete = true
			case "turn.failed":
				protocolErr = errors.New("Codex reported failed execution")
				complete = false
			case "error":
				// Codex also emits error diagnostics while reconnecting. A
				// later completed turn is authoritative; a failed turn or
				// missing terminal event remains a failure below.
			}
		} else {
			if envelope.SessionID != "" {
				result.SessionID = envelope.SessionID
			}
			if envelope.Type == "result" {
				if envelope.IsError || envelope.Subtype != "success" {
					protocolErr = errors.New("Claude reported a failed or unknown result")
				} else {
					complete = true
					result.OutputText = envelope.Result
				}
			}
		}
		if len(result.OutputText) > 8<<20 {
			protocolErr = errors.New("agent result exceeds size limit")
			s.Stop()
		}
	}
	status, waitErr := s.Wait()
	result.ExitCode, result.TreeStopped = status.ExitCode, status.TreeStopped
	if !complete || result.SessionID == "" {
		protocolErr = errors.Join(protocolErr, ErrProtocol)
	}
	return result, errors.Join(waitErr, protocolErr, <-inputDone)
}

func commandArgs(req Request) []string {
	if req.Tool == "codex" {
		sandbox := "workspace-write"
		if req.Role == "review" || req.Role == "reviewer" {
			sandbox = "read-only"
		}
		args := []string{"--ask-for-approval", "never", "exec", "--json", "--sandbox", sandbox}
		if req.ResumeSession != "" {
			args = append(args, "resume", req.ResumeSession)
		}
		return append(args, "--", "-")
	}
	args := []string{"-p", "--output-format", "stream-json", "--verbose", "--permission-mode", "dontAsk"}
	tools := req.AllowedTools
	if len(tools) == 0 {
		tools = []string{"Read", "Glob", "Grep"}
	}
	// dontAsk denies everything that would otherwise ask, while these explicit
	// rules cover the task's authorized tools. Never pass bypassPermissions.
	args = append(args, "--allowedTools", strings.Join(tools, ","))
	if req.Role == "review" || req.Role == "reviewer" {
		args = append(args, "--tools", "Read,Glob,Grep")
	}
	if req.ResumeSession != "" {
		args = append(args, "--resume", req.ResumeSession)
	}
	return args
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
