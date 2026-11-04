// Package acpclient provides bounded ACP v1 connections, Session creation and
// explicit text prompts. Task scheduling and application authorization belong
// to the calling runtime layer.
package acpclient

import (
	"encoding/json"
	"errors"
	"fmt"

	"coordica/internal/process"
)

const maxFrameBytes = 256 << 10

var (
	ErrProtocol           = errors.New("invalid ACP protocol")
	ErrDisconnected       = errors.New("ACP connection ended")
	ErrOutcomeUnknown     = errors.New("ACP request outcome is unknown")
	ErrUnsupportedVersion = errors.New("unsupported ACP protocol version")
	ErrUnsupported        = errors.New("ACP operation is not implemented by this client")
	ErrSessionAllocated   = errors.New("ACP peer already consumed its session allocation attempt")
	ErrUnsupportedUpdate  = errors.New("ACP update or callback is not implemented by this client")
	ErrBackpressure       = errors.New("ACP local update capture capacity exhausted")
)

// Config is trusted host wiring. Args/Env are explicit, copied selections, not
// Task inputs or proof of native authentication, permissions or OS isolation.
type Config struct {
	Executable string
	Args       []string
	Directory  string
	Env        []string
}

type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
type AuthMethod struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type AgentCapabilities struct {
	LoadSession bool
	Prompt      struct{ Audio, Image, EmbeddedContext bool }
	MCP         struct{ HTTP, SSE bool }
}

// Info contains bounded agent declarations. They do not prove authentication,
// method implementation, effective permissions or provider compatibility.
type Info struct {
	ProtocolVersion   int
	AgentInfo         *Implementation
	AgentCapabilities AgentCapabilities
	AuthMethods       []AuthMethod
}

type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Error omits native text/data, which can contain private material.
func (e *RPCError) Error() string { return fmt.Sprintf("ACP RPC error %d", e.Code) }

// InitializationError retains actual owned-process cleanup evidence separately
// from the initialization outcome. TreeStopped does not prove authentication.
type InitializationError struct {
	Cause   error
	Process process.Result
}

func (e *InitializationError) Error() string { return "ACP initialization failed: " + e.Cause.Error() }
func (e *InitializationError) Unwrap() error { return e.Cause }

// SessionCreationError keeps possible allocation and local process-stop facts
// separate. TreeStopped does not prove the native/remote Session was deleted.
type SessionCreationError struct {
	Cause           error
	NativeSessionID string
	Process         process.Result
}

func (e *SessionCreationError) Error() string {
	return "ACP session creation failed: " + e.Cause.Error()
}
func (e *SessionCreationError) Unwrap() error { return e.Cause }
