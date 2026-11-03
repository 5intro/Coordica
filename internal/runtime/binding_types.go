package runtime

import (
	"context"
	"encoding/json"
	"errors"
)

var (
	ErrOutcomeUnknown  = errors.New("runtime external outcome is unknown")
	ErrStopUnconfirmed = errors.New("runtime binding stop is unconfirmed")
)

const (
	kindBinding        = "runtime.binding"
	kindBindingRun     = "runtime.binding-run"
	kindBindingCommand = "runtime.binding-command"
)

// BindingConfig is resolved by the trusted host, never from task/context text.
// References and fingerprints do not attest login health or OS isolation.
type BindingConfig struct {
	Target               Target `json:"target"`
	InstanceConfigRef    string `json:"instance_config_ref"`
	ProfileRef           string `json:"profile_ref"`
	WorkspaceFingerprint string `json:"workspace_fingerprint"`
	ProtocolVersion      string `json:"protocol_version"`
	ApprovalPolicy       string `json:"approval_policy"`
	Sandbox              string `json:"sandbox"`
}

type NativeSessionID struct {
	Harness string `json:"harness"`
	Value   string `json:"value"`
}

type Binding struct {
	ID                   string          `json:"id"`
	ApplicationID        string          `json:"application_id"`
	TaskID               string          `json:"task_id"`
	RunID                string          `json:"run_id"`
	RunGeneration        int64           `json:"run_generation"`
	ContextID            string          `json:"context_id"`
	ContextVersion       int64           `json:"context_version"`
	AuthorizationVersion int64           `json:"authorization_version"`
	PrincipalID          string          `json:"principal_id"`
	Config               BindingConfig   `json:"config"`
	Version              int64           `json:"version"`
	Generation           int64           `json:"binding_generation"`
	OwnerID              string          `json:"owner_id"`
	CommandID            string          `json:"command_id"`
	State                string          `json:"state"`
	NativeID             NativeSessionID `json:"native_id"`
	Outcome              string          `json:"outcome"`
	CleanupStopped       bool            `json:"cleanup_stopped"`
}

type BindingCommand struct {
	ID         string         `json:"id"`
	BindingID  string         `json:"binding_id"`
	Generation int64          `json:"binding_generation"`
	OwnerID    string         `json:"owner_id"`
	Operation  string         `json:"operation"`
	State      string         `json:"state"`
	Observed   NativeCreation `json:"observed"`
	Stop       BindingStop    `json:"stop"`
}

type PrepareBindingRequest struct {
	Key             string `json:"key"`
	TaskID          string `json:"task_id"`
	RunID           string `json:"run_id"`
	RunGeneration   int64  `json:"run_generation"`
	ExpectedVersion int64  `json:"expected_task_version"`
}

type BindingRequest struct {
	Key             string `json:"key"`
	BindingID       string `json:"binding_id"`
	ExpectedVersion int64  `json:"expected_version"`
	Generation      int64  `json:"binding_generation"`
}

// NativeCreate contains no task/context body, prompt, Turn or arbitrary method.
type NativeCreate struct {
	BindingID  string
	CommandID  string
	OwnerID    string
	Generation int64
	Config     BindingConfig
}

// Outcome is confirmed|rejected|not_sent|unknown. A returned native identity on
// an error is an observed resource, not proof that creation was rejected.
type NativeCreation struct {
	Outcome       string          `json:"outcome"`
	ID            NativeSessionID `json:"id"`
	Effective     BindingConfig   `json:"effective"`
	Configuration json.RawMessage `json:"configuration,omitempty"` // Private native configuration evidence, never history.
}

type BindingStop struct {
	BindingID   string          `json:"binding_id"`
	OwnerID     string          `json:"owner_id"`
	NativeID    NativeSessionID `json:"native_id"`
	TreeStopped bool            `json:"tree_stopped"`
}

// BindingPort is trusted host wiring. Implementations must scope Stop to this
// exact binding/owner, preserve structured rejection/not-sent facts, and return
// real process-tree evidence. This slice supplies no production harness port.
type BindingPort interface {
	Create(context.Context, NativeCreate) (NativeCreation, error)
	Stop(context.Context, NativeCreate, NativeSessionID) (BindingStop, error)
}

// SessionPort is an optional, consumer-owned extension. Read uses an existing
// owned handle; Resume restores only the persisted native context, never work.
type SessionPort interface {
	BindingPort
	Resume(context.Context, NativeCreate, NativeSessionID) (NativeCreation, error)
	Read(context.Context, NativeCreate, NativeSessionID, bool) (NativeSnapshot, error)
}

type NativeSnapshot struct {
	Outcome string
	History json.RawMessage // Private transient data: never an audit or receipt body.
}
