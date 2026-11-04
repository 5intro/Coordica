package acpclient

import (
	"encoding/json"
	"errors"

	"coordica/internal/process"
)

var (
	ErrStalePermission         = errors.New("ACP permission request is no longer answerable")
	ErrInvalidPermissionAnswer = errors.New("invalid ACP permission option selection")
	ErrUnsupportedPermission   = errors.New("ACP permission request exceeds the supported client subset")
	ErrPermissionUnresolved    = errors.New("ACP prompt ended with unresolved permission requests")
)

// PermissionOption retains an agent-provided choice. Kind is a native hint,
// never application authority or a locally remembered permission policy.
type PermissionOption struct {
	ID, Name, Kind string
	rawID          json.RawMessage // Original native JSON value, never reconstructed from display text.
}

// PermissionRequest is a private, invocation-bound request handle. Its native
// contents can contain sensitive text; they are not public event metadata.
type PermissionRequest struct {
	session  Session
	serial   uint64
	id       json.RawMessage
	toolCall json.RawMessage
	options  []PermissionOption
}

// ID preserves the native JSON-RPC value and type, not a native Turn identity.
func (r PermissionRequest) ID() json.RawMessage {
	return append(json.RawMessage(nil), r.id...)
}

// ToolCall returns private native details, not a verified authorization scope.
func (r PermissionRequest) ToolCall() json.RawMessage {
	return append(json.RawMessage(nil), r.toolCall...)
}

func (r PermissionRequest) Options() []PermissionOption {
	options := append([]PermissionOption{}, r.options...)
	for i := range options {
		options[i].rawID = append(json.RawMessage(nil), options[i].rawID...)
	}
	return options
}

// PermissionReceipt records local transport facts only. Evidence local_write
// and Written do not acknowledge native acceptance, execution or tool stopping.
type PermissionReceipt struct {
	LocalRequest      uint64
	Evidence, Outcome string
	Written           bool
}

type PermissionReplyError struct {
	Cause   error
	Process process.Result
}

// Error deliberately excludes Cause, which may contain private native content.
func (e *PermissionReplyError) Error() string { return "ACP permission reply failed" }
func (e *PermissionReplyError) Unwrap() error { return e.Cause }
