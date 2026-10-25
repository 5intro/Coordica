package domain

import "time"

// Initialization is independent of Goal and never grants execution authority.
type Baseline struct {
	Requirements   string   `json:"requirements"`
	Acceptance     string   `json:"acceptance"`
	Architecture   string   `json:"architecture"`
	Modules        string   `json:"modules"`
	Conventions    string   `json:"conventions"`
	Testing        string   `json:"testing"`
	Functionality  string   `json:"functionality"`
	Risks          string   `json:"risks"`
	AllowedRoots   []string `json:"allowed_roots"`
	ProtectedPaths []string `json:"protected_paths"`
	Checks         []Check  `json:"checks"`
}
type InitializationQuestion struct {
	ID            string   `json:"id"`
	Text          string   `json:"text"`
	Prerequisites []string `json:"prerequisites"`
}
type AnalysisEvidence struct {
	Path string `json:"path"`
	Fact string `json:"fact"`
}
type InitializationResult struct {
	Generation            int64                    `json:"generation"`
	InitializationVersion int64                    `json:"initialization_version"`
	OperationID           string                   `json:"operation_id"`
	SourceSHA             string                   `json:"source_sha"`
	Ready                 bool                     `json:"ready"`
	InspectedPaths        []string                 `json:"inspected_paths"`
	Facts                 []AnalysisEvidence       `json:"facts"`
	Suggestions           []string                 `json:"suggestions"`
	Questions             []InitializationQuestion `json:"questions"`
	Baseline              Baseline                 `json:"baseline"`
}
type InitializationFeedback struct {
	ID           string     `json:"id"`
	Version      int64      `json:"version"`
	ActorID      string     `json:"actor_id"`
	QuestionID   string     `json:"question_id"`
	OriginalText string     `json:"original_text"`
	Source       ChatSource `json:"source"`
	CreatedAt    time.Time  `json:"created_at"`
}
type Initialization struct {
	Leader              *LeaderSelection         `json:"leader,omitempty"`
	Version             int64                    `json:"version"`
	Status              string                   `json:"status"`
	Kind                string                   `json:"kind"`
	SourceSHA           string                   `json:"source_sha"`
	PolicyDigest        string                   `json:"policy_digest"`
	AvailablePaths      []string                 `json:"available_paths"`
	CoveredPaths        []string                 `json:"covered_paths"`
	Omissions           []string                 `json:"omissions"`
	Result              *InitializationResult    `json:"result"`
	Feedback            []InitializationFeedback `json:"feedback"`
	Digest              string                   `json:"digest"`
	ApprovedVersion     int64                    `json:"approved_version"`
	ApprovedDigest      string                   `json:"approved_digest"`
	ApprovedBaseline    *Baseline                `json:"approved_baseline"`
	OperationID         string                   `json:"operation_id"`
	OperationGeneration int64                    `json:"operation_generation"`
	OperationVersion    int64                    `json:"operation_version"`
	OperationState      string                   `json:"operation_state"`
	Detail              string                   `json:"detail"`
}
type InitializationRequest struct {
	ExpectedVersion int64            `json:"expected_version"`
	Kind            string           `json:"kind"`
	Leader          *LeaderSelection `json:"leader,omitempty"`
	LeaderVersion   *int64           `json:"leader_version,omitempty"`
}
type InitializationAnswer struct {
	ExpectedVersion int64      `json:"expected_version"`
	QuestionID      string     `json:"question_id"`
	OriginalText    string     `json:"original_text"`
	Source          ChatSource `json:"source"`
}
type InitializationConfirm struct {
	ExpectedVersion int64  `json:"expected_version"`
	Digest          string `json:"digest"`
}

// Internal process/accounting record; its PID and working directory are not
// part of the limited interaction projection.
type InitializationOperation struct {
	Leader                *LeaderSelection `json:"leader,omitempty"`
	ID                    string           `json:"id"`
	ActorID               string           `json:"actor_id"`
	ActorRole             string           `json:"actor_role"`
	Version               int64            `json:"version"`
	InitializationVersion int64            `json:"initialization_version"`
	Generation            int64            `json:"generation"`
	State                 string           `json:"state"`
	MaxRetries            int              `json:"max_retries"`
	Attempt               int              `json:"attempt"`
	ProcessID             int              `json:"process_id"`
	WorkingDirectory      string           `json:"working_directory"`
	Deadline              time.Time        `json:"deadline"`
	GrantSeconds          int64            `json:"grant_seconds"`
	UsedSeconds           int64            `json:"used_seconds"`
	ElapsedSeconds        int64            `json:"elapsed_seconds"`
	TreeStopped           bool             `json:"tree_stopped"`
	Settled               bool             `json:"settled"`
}

type ProjectPolicyRequest struct {
	ExpectedVersion int64    `json:"expected_version"`
	Checks          []Check  `json:"checks"`
	ProtectedPaths  []string `json:"protected_paths"`
}

type CenterIdentity struct {
	InstanceID   string `json:"instance_id"`
	ConfigDigest string `json:"config_digest"`
	DataDir      string `json:"data_dir"`
}
type CenterRuntimeStatus struct {
	Identity            CenterIdentity `json:"identity"`
	ProjectPaused       bool           `json:"project_paused"`
	AutomationEnabled   bool           `json:"automation_enabled"`
	RegisteredRunners   int            `json:"registered_runners"`
	LastRunnerHeartbeat *time.Time     `json:"last_runner_heartbeat"`
}

type CenterHealth struct {
	Status           string `json:"status"`
	APIVersion       string `json:"api_version"`
	CenterInstanceID string `json:"center_instance_id,omitempty"`
	ConfigDigest     string `json:"config_digest,omitempty"`
}

type UserServiceStatus struct {
	Name  string `json:"name"`
	State string `json:"state"`
}
type HostLifecycleStatus struct {
	Role                  string            `json:"host_role"`
	Configured            bool              `json:"configured"`
	Initialized           bool              `json:"initialized"`
	Service               UserServiceStatus `json:"service"`
	MatchingCenterHealthy bool              `json:"matching_center_healthy"`
	ChatReady             *bool             `json:"chat_ready"`
	ProjectPaused         *bool             `json:"project_paused"`
	AutomationEnabled     *bool             `json:"automation_enabled"`
	RegisteredRunners     *int              `json:"registered_runners"`
	LastRunnerHeartbeat   *time.Time        `json:"last_runner_heartbeat"`
	ConsoleURL            string            `json:"console_url"`
	Detail                string            `json:"detail"`
}
