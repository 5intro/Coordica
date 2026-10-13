package domain

import "time"

const ConsultationPurpose = "consultation"
const ChatScopeCenter = "center"
const ChatScopeNode = "node"

type ChatSource struct {
	ClientID  string `json:"client_id,omitempty"`
	SessionID string `json:"session_id"`
	MessageID string `json:"message_id"`
}
type ChatCredentialRequest struct {
	Name string `json:"name"`
}
type ChatCredentialInfo struct {
	ChatScope string    `json:"chat_scope"`
	NodeID    string    `json:"node_id,omitempty"`
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Revoked   bool      `json:"revoked"`
	CreatedAt time.Time `json:"created_at"`
}

// Only authenticated Owner/paired-node issuance responses contain a bearer token.
type ChatCredentialGrant struct {
	Credential ChatCredentialInfo `json:"credential"`
	Token      string             `json:"token"`
}
type ConversationInput struct {
	ExpectedGoalVersion int64      `json:"expected_goal_version"`
	OriginalText        string     `json:"original_text"`
	AssistantSummary    string     `json:"assistant_summary,omitempty"`
	Source              ChatSource `json:"source"`
	ReplyTo             string     `json:"reply_to,omitempty"`
}
type ChatRequirementInput struct {
	Title            string     `json:"title"`
	OriginalText     string     `json:"original_text"`
	AssistantSummary string     `json:"assistant_summary,omitempty"`
	Source           ChatSource `json:"source"`
}
type ConsultResult struct {
	Reply string `json:"reply"`
}
type ConversationRequest struct {
	ID               string     `json:"id"`
	Version          int64      `json:"version"`
	Kind             string     `json:"kind"`
	OriginalText     string     `json:"original_text"`
	AssistantSummary string     `json:"assistant_summary"`
	Source           ChatSource `json:"source"`
	ReplyTo          string     `json:"reply_to,omitempty"`
	GoalVersion      int64      `json:"goal_version"`
	CreatedAt        time.Time  `json:"created_at"`
	RunID            string     `json:"run_id"`
	Status           string     `json:"status"`
	Reply            string     `json:"reply"`
	Stale            bool       `json:"stale"`
	ResultSHA256     string     `json:"result_sha256"`
	RepliedAt        *time.Time `json:"replied_at,omitempty"`
}

// Cumulative consultation telemetry and reservation target. BudgetSeconds is deprecated and ignored.
type ConsultationAccount struct {
	ID              string `json:"id"`
	BudgetSeconds   int64  `json:"budget_seconds"`
	UsedSeconds     int64  `json:"used_seconds"`
	ReservedSeconds int64  `json:"reserved_seconds"`
}
type ChatOverview struct {
	InitializationStatus     string               `json:"initialization_status"`
	ApprovedBaselineVersion  int64                `json:"approved_baseline_version"`
	ApprovedBaselineDigest   string               `json:"approved_baseline_digest"`
	ChatScope                string               `json:"chat_scope"`
	NodeID                   string               `json:"node_id,omitempty"`
	ApprovedGoalVersion      int64                `json:"approved_goal_version"`
	GoalAuthorizationVersion int64                `json:"goal_authorization_version"`
	DirectoryOutline         string               `json:"directory_outline"`
	AllowedRoots             []string             `json:"allowed_roots"`
	RequirementIDs           []string             `json:"requirement_ids"`
	ConsultationAccount      *ConsultationAccount `json:"consultation_account,omitempty"`

	APIVersion   string    `json:"api_version"`
	QueriedAt    time.Time `json:"queried_at"`
	ProjectID    string    `json:"project_id"`
	Version      int64     `json:"version"`
	Name         string    `json:"name"`
	Paused       bool      `json:"paused"`
	GoalVersion  int64     `json:"goal_version"`
	GoalStatus   string    `json:"goal_status"`
	Objective    string    `json:"objective"`
	Architecture string    `json:"architecture"`
	NonGoals     string    `json:"non_goals"`
	Acceptance   string    `json:"acceptance"`
	// Deprecated: retained for wire compatibility; ignored project allowance.
	BudgetSeconds   int64 `json:"budget_seconds"`
	UsedSeconds     int64 `json:"used_seconds"`
	ReservedSeconds int64 `json:"reserved_seconds"`
}
type ChatListQuery struct {
	Kind   string `json:"kind"`
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit"`
}
type ChatItemQuery struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}
type ChatRequestQuery struct {
	ID string `json:"id"`
}
type ChatCancelRequest struct {
	ID              string `json:"id"`
	ExpectedVersion int64  `json:"expected_version"`
}

// Deliberately excludes executable definitions, command lines, paths, raw
// outcomes, authentication, publication material and owner settings.
type ChatAgentCapability struct {
	Structured bool   `json:"structured"`
	Resume     bool   `json:"resume"`
	Cancel     bool   `json:"cancel"`
	Tool       string `json:"tool"`
	Version    string `json:"version"`
	Protocol   string `json:"protocol"`
	Available  bool   `json:"available"`
	ReadOnly   bool   `json:"read_only"`
	Review     bool   `json:"review"`
}
type ChatItem struct {
	SourceNodeID string                `json:"source_node_id,omitempty"`
	Runs         []ChatRun             `json:"runs,omitempty"`
	Run          *ChatRun              `json:"run,omitempty"`
	Capacity     int                   `json:"capacity,omitempty"`
	ObservedAt   *time.Time            `json:"observed_at,omitempty"`
	Capabilities []ChatAgentCapability `json:"capabilities,omitempty"`
	Truncated    bool                  `json:"truncated,omitempty"`

	RequirementIDs       []string `json:"requirement_ids,omitempty"`
	Dependencies         []string `json:"dependencies,omitempty"`
	AllowedPaths         []string `json:"allowed_paths,omitempty"`
	Tool                 string   `json:"tool,omitempty"`
	ApprovedVersion      int64    `json:"approved_version,omitempty"`
	AuthorizationVersion int64    `json:"authorization_version,omitempty"`
	BudgetSeconds        int64    `json:"budget_seconds,omitempty"`
	UsedSeconds          int64    `json:"used_seconds,omitempty"`
	ReservedSeconds      int64    `json:"reserved_seconds,omitempty"`

	ID           string               `json:"id"`
	Version      int64                `json:"version"`
	Kind         string               `json:"kind"`
	Title        string               `json:"title"`
	Text         string               `json:"text"`
	Summary      string               `json:"summary"`
	Status       string               `json:"status"`
	GoalVersion  int64                `json:"goal_version"`
	Source       *ChatSource          `json:"source,omitempty"`
	Conversation *ConversationRequest `json:"conversation,omitempty"`
}
type ChatPage struct {
	QueriedAt  time.Time  `json:"queried_at"`
	Version    int64      `json:"version"`
	Items      []ChatItem `json:"items"`
	NextCursor string     `json:"next_cursor"`
}
type ChatDetail struct {
	QueriedAt time.Time `json:"queried_at"`
	Version   int64     `json:"version"`
	Item      ChatItem  `json:"item"`
}

// Safe execution evidence. Historical ownership never grants access to a task's
// subsequent assignment, its current goal or private execution output.
type ChatRun struct {
	ID             string     `json:"id"`
	Version        int64      `json:"version"`
	State          string     `json:"state"`
	Tool           string     `json:"tool"`
	Kind           string     `json:"kind"`
	NodeID         string     `json:"node_id"`
	StartedAt      time.Time  `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	Deadline       time.Time  `json:"deadline"`
	LeaseUntil     time.Time  `json:"lease_until"`
	GrantSeconds   int64      `json:"grant_seconds"`
	ElapsedSeconds int64      `json:"elapsed_seconds"`
	Quiet          bool       `json:"quiet"`
	TaskTitle      string     `json:"task_title,omitempty"`
	TaskText       string     `json:"task_text,omitempty"`
}
