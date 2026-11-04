// Package runtime provides the experimental, local durable task ledger.
// It records explicit work; it does not start harnesses or dispatch tasks.
package runtime

import "time"

type Action string

const (
	Submit         Action = "task.submit"
	Read           Action = "task.read"
	WriteContext   Action = "task.context"
	Register       Action = "run.register"
	Continue       Action = "run.continue"
	Cancel         Action = "task.cancel"
	SessionControl Action = "session.control"
)

// Credential is presented by an in-process caller, not inferred from
// chat/profile identity. Tokens are never included in command digests or audit.
type Credential struct {
	PrincipalID string `json:"-"`
	Token       string `json:"-"`
}

type TargetScope struct {
	InstanceID string `json:"instance_id"`
	NodeID     string `json:"node_id"`
}

type Grant struct {
	ID            string        `json:"id"`
	ApplicationID string        `json:"application_id"`
	Version       int64         `json:"version"`
	Enabled       bool          `json:"enabled"`
	Actions       []Action      `json:"actions"`
	Targets       []TargetScope `json:"targets"`
}

type GrantConfig struct {
	Grant
	Token string `json:"-"`
}

// Instance is a host-registered identity/configuration reference. Support is
// supported|unsupported|unknown|conditional metadata, never execution authority.
type Instance struct {
	ID            string `json:"id"`
	ApplicationID string `json:"application_id"`
	NodeID        string `json:"node_id"`
	Harness       string `json:"harness"`
	ConfigRef     string `json:"config_ref"`
	Support       string `json:"support"`
	Version       int64  `json:"version"`
	Enabled       bool   `json:"enabled"`
}

type Task struct {
	ID             string    `json:"id"`
	ApplicationID  string    `json:"application_id"`
	ExternalRef    string    `json:"external_ref"`
	Title          string    `json:"title"`
	State          string    `json:"state"`
	Version        int64     `json:"version"`
	ContextID      string    `json:"context_id"`
	ContextVersion int64     `json:"context_version"`
	CurrentRunID   string    `json:"current_run_id"`
	RunGeneration  int64     `json:"run_generation"`
	CreatedAt      time.Time `json:"created_at"`
}

// TaskContext snapshots are immutable. Runs retain the chosen snapshot ID even
// when later context updates advance the Task's current version.
type TaskContext struct {
	ID            string    `json:"id"`
	ApplicationID string    `json:"application_id"`
	TaskID        string    `json:"task_id"`
	Version       int64     `json:"version"`
	Content       string    `json:"content"`
	CreatedAt     time.Time `json:"created_at"`
}

type TaskSnapshot struct {
	Task    Task        `json:"task"`
	Context TaskContext `json:"context"`
}

type Target struct {
	InstanceID      string `json:"instance_id"`
	InstanceVersion int64  `json:"instance_version"`
	NodeID          string `json:"node_id"`
	Harness         string `json:"harness"`
	Model           string `json:"model"`
	Effort          string `json:"effort"`
	WorkspaceRef    string `json:"workspace_ref"`
}

type Run struct {
	ID                   string    `json:"id"`
	ApplicationID        string    `json:"application_id"`
	TaskID               string    `json:"task_id"`
	Version              int64     `json:"version"`
	Attempt              int64     `json:"attempt"`
	Generation           int64     `json:"generation"`
	Target               Target    `json:"target"`
	InstanceConfigRef    string    `json:"instance_config_ref"`
	ContextID            string    `json:"context_id"`
	ContextVersion       int64     `json:"context_version"`
	AuthorizationVersion int64     `json:"authorization_version"`
	PrincipalID          string    `json:"principal_id"`
	State                string    `json:"state"`
	NativeDisposition    string    `json:"native_disposition"`
	CreatedAt            time.Time `json:"created_at"`
}

type RunSnapshot struct {
	Run     Run         `json:"run"`
	Context TaskContext `json:"context"`
}

type Source struct {
	ID      string `json:"id"`
	EventID string `json:"event_id"`
}

// Input is an immutable source fact. Continuations remain recorded, not
// delivered/consumed, and cannot silently attach to a different Run.
type Input struct {
	ID             string    `json:"id"`
	ApplicationID  string    `json:"application_id"`
	TaskID         string    `json:"task_id"`
	Source         Source    `json:"source"`
	Kind           string    `json:"kind"`
	Content        string    `json:"content"`
	Digest         string    `json:"digest"`
	RunID          string    `json:"run_id,omitempty"`
	RunGeneration  int64     `json:"run_generation,omitempty"`
	ContextID      string    `json:"context_id"`
	ContextVersion int64     `json:"context_version"`
	Disposition    string    `json:"disposition"`
	TaskVersion    int64     `json:"task_version"`
	CreatedAt      time.Time `json:"created_at"`
}

type SubmitTaskRequest struct {
	Key         string `json:"key"`
	ExternalRef string `json:"external_ref"`
	Title       string `json:"title"`
	Context     string `json:"context"`
}

type ContextRequest struct {
	Key                    string `json:"key"`
	TaskID                 string `json:"task_id"`
	ExpectedVersion        int64  `json:"expected_version"`
	ExpectedContextVersion int64  `json:"expected_context_version"`
	Source                 Source `json:"source"`
	Content                string `json:"content"`
}

type RegisterRunRequest struct {
	Key                    string `json:"key"`
	TaskID                 string `json:"task_id"`
	ExpectedVersion        int64  `json:"expected_version"`
	ExpectedContextVersion int64  `json:"expected_context_version"`
	Target                 Target `json:"target"`
}

type ContinuationRequest struct {
	Key             string `json:"key"`
	TaskID          string `json:"task_id"`
	ExpectedVersion int64  `json:"expected_version"`
	RunID           string `json:"run_id"`
	RunGeneration   int64  `json:"run_generation"`
	ContextVersion  int64  `json:"context_version"`
	Source          Source `json:"source"`
	Content         string `json:"content"`
}

type CancelRequest struct {
	Key             string `json:"key"`
	TaskID          string `json:"task_id"`
	ExpectedVersion int64  `json:"expected_version"`
	RunID           string `json:"run_id,omitempty"`
	RunGeneration   int64  `json:"run_generation,omitempty"`
}
