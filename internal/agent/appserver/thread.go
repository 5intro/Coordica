package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"coordica/internal/jsoninput"
)

var (
	ErrInvalidTarget = errors.New("invalid native app-server target")
	ErrConfiguration = errors.New("native app-server configuration differs from the explicit selection")
)

// ThreadOptions selects native configuration, not authority to execute a Task.
// No model or permission fallback is supplied by this adapter. The trusted Peer
// workspace is used; arbitrary task-provided directories are not accepted here.
type ThreadOptions struct {
	Model          string
	ApprovalPolicy string
	Sandbox        string
}

// Configuration preserves what the native server reports. A configured mode is
// not an OS isolation attestation. Raw values require redaction before display.
type Configuration struct {
	NativeThreadID string          `json:"-"`
	Model          string          `json:"model"`
	ModelProvider  string          `json:"modelProvider"`
	Directory      string          `json:"cwd"`
	ApprovalPolicy json.RawMessage `json:"approvalPolicy"`
	Sandbox        json.RawMessage `json:"sandbox"`
}

// Thread is an immutable native target owned by one live transport. It is not
// the durable Coordica Task, logical Thread, or a transferable session handle.
type Thread struct {
	peer *Peer
	id   string
}

func (t Thread) ID() string { return t.id }

// StartThread creates only native context. It does not send work or start a Turn.
func StartThread(ctx context.Context, peer *Peer, options ThreadOptions) (Thread, Configuration, error) {
	return openThread(ctx, peer, "thread/start", "", options)
}

// ResumeThread binds a specific native ID without replaying its last input.
// The caller must first revalidate persisted instance/workspace/authority fences.
func ResumeThread(ctx context.Context, peer *Peer, id string, options ThreadOptions) (Thread, Configuration, error) {
	if !validNativeID(id) {
		return Thread{}, Configuration{}, ErrInvalidTarget
	}
	return openThread(ctx, peer, "thread/resume", id, options)
}

func openThread(ctx context.Context, peer *Peer, method, id string, options ThreadOptions) (Thread, Configuration, error) {
	if peer == nil || !options.valid() {
		return Thread{}, Configuration{}, ErrConfiguration
	}
	params := map[string]any{"model": options.Model, "approvalPolicy": options.ApprovalPolicy,
		"sandbox": options.Sandbox, "cwd": peer.directory}
	if id == "" {
		params["allowProviderModelFallback"] = false
	} else {
		params["threadId"] = id
	}
	var raw json.RawMessage
	if err := peer.Call(ctx, method, params, &raw); err != nil {
		return Thread{}, Configuration{}, err
	}
	var result struct {
		Configuration
		Thread json.RawMessage `json:"thread"`
	}
	if jsoninput.Required(raw, "thread", "model", "modelProvider", "cwd", "approvalPolicy", "sandbox") != nil ||
		json.Unmarshal(raw, &result) != nil {
		return Thread{}, Configuration{}, peer.invalidResult()
	}
	got, err := threadID(result.Thread)
	if err != nil || id != "" && got != id || result.Model == "" || result.ModelProvider == "" || !filepath.IsAbs(result.Directory) {
		return Thread{}, result.Configuration, peer.invalidResult()
	}
	result.NativeThreadID = got
	if !options.matches(result.Configuration) || filepath.Clean(result.Directory) != peer.directory {
		// No Turn has been started. Preserve the reported configuration for
		// reconciliation; do not retry, silently accept it, or start work.
		return Thread{}, result.Configuration, ErrConfiguration
	}
	return Thread{peer: peer, id: got}, result.Configuration, nil
}

// Read retrieves native history, never starts work and never installs a binding.
// Its raw snapshot may include private messages and needs caller-owned redaction.
func (t Thread) Read(ctx context.Context, includeTurns bool) (json.RawMessage, error) {
	if !t.valid() {
		return nil, ErrInvalidTarget
	}
	var result struct {
		Thread json.RawMessage `json:"thread"`
	}
	if err := t.peer.Call(ctx, "thread/read", map[string]any{"threadId": t.id, "includeTurns": includeTurns}, &result); err != nil {
		return nil, err
	}
	id, err := threadID(result.Thread)
	if err != nil || id != t.id {
		return nil, t.peer.invalidResult()
	}
	return result.Thread, nil
}

func (t Thread) valid() bool { return t.peer != nil && validNativeID(t.id) }

func threadID(raw json.RawMessage) (string, error) {
	var value struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(raw, &value) != nil || !validNativeID(value.ID) {
		return "", ErrProtocol
	}
	return value.ID, nil
}

func validNativeID(id string) bool {
	return id != "" && len(id) <= 512 && utf8.ValidString(id) &&
		!strings.ContainsFunc(id, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

func (o ThreadOptions) valid() bool {
	return validNativeID(o.Model) && (o.ApprovalPolicy == "untrusted" || o.ApprovalPolicy == "on-request" || o.ApprovalPolicy == "never") &&
		(o.Sandbox == "read-only" || o.Sandbox == "workspace-write" || o.Sandbox == "danger-full-access")
}

func (o ThreadOptions) matches(c Configuration) bool {
	var approval string
	var sandbox struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(c.ApprovalPolicy, &approval) != nil || json.Unmarshal(c.Sandbox, &sandbox) != nil {
		return false
	}
	types := map[string]string{"read-only": "readOnly", "workspace-write": "workspaceWrite", "danger-full-access": "dangerFullAccess"}
	return c.Model == o.Model && approval == o.ApprovalPolicy && sandbox.Type == types[o.Sandbox]
}

func (p *Peer) invalidResult() error {
	p.fail(ErrProtocol)
	return errors.Join(ErrOutcomeUnknown, ErrProtocol)
}
