package domain

import "time"

// Goal authorization is independent of the mandatory shared project baseline.
type AutomationSettings struct {
	Version                int64  `json:"version"`
	Enabled                bool   `json:"enabled"`
	DevelopmentTool        string `json:"development_tool"`
	DevelopmentModel       string `json:"development_model"`
	LeaderTool             string `json:"leader_tool"`
	LeaderModel            string `json:"leader_model"`
	PlanningTimeoutSeconds int64  `json:"planning_timeout_seconds"`
	PlanningBudgetSeconds  int64  `json:"planning_budget_seconds"`
	PlanningMaxRetries     int    `json:"planning_max_retries"`
}

func DefaultAutomationSettings() AutomationSettings {
	return AutomationSettings{Version: 1, DevelopmentTool: "codex", DevelopmentModel: "gpt-6.1-sol", LeaderTool: "codex", LeaderModel: "gpt-6.1-sol", PlanningTimeoutSeconds: 300, PlanningBudgetSeconds: 900, PlanningMaxRetries: 2}
}

type AutomationRequest struct {
	ExpectedVersion int64              `json:"expected_version"`
	Settings        AutomationSettings `json:"settings"`
}
type PlanningDocument struct {
	ApprovedAuthorizationVersion int64         `json:"approved_authorization_version"`
	Version                      int64         `json:"version"`
	AuthorizationVersion         int64         `json:"authorization_version"`
	ApprovedVersion              int64         `json:"approved_version"`
	Objective                    string        `json:"objective"`
	NonGoals                     string        `json:"non_goals"`
	Acceptance                   string        `json:"acceptance"`
	RequirementIDs               []string      `json:"requirement_ids"`
	Architecture                 string        `json:"architecture"`
	DirectoryOutline             string        `json:"directory_outline"`
	AllowedRoots                 []string      `json:"allowed_roots"`
	BudgetSeconds                int64         `json:"budget_seconds"`
	UsedSeconds                  int64         `json:"used_seconds"`
	ReservedSeconds              int64         `json:"reserved_seconds"`
	PlanningUsedSeconds          int64         `json:"planning_used_seconds"`
	PlanningReservedSeconds      int64         `json:"planning_reserved_seconds"`
	Status                       string        `json:"status"`
	AdmittedDocumentVersion      int64         `json:"admitted_document_version"`
	AdmittedAuthorizationVersion int64         `json:"admitted_authorization_version"`
	MissingItems                 []string      `json:"missing_items"`
	LeaderResult                 *LeaderResult `json:"leader_result,omitempty"`
	LeaderSession                string        `json:"leader_session"`
}
type PlanningSaveRequest struct {
	ExpectedVersion int64            `json:"expected_version"`
	Document        PlanningDocument `json:"document"`
}
type LeaderMessage struct {
	ID              string    `json:"id"`
	DocumentVersion int64     `json:"document_version"`
	Role            string    `json:"role"`
	Text            string    `json:"text"`
	CreatedAt       time.Time `json:"created_at"`
}
type PlanningMessageRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	Text            string `json:"text"`
}
type TaskProposal struct {
	TestRunID      string   `json:"test_run_id,omitempty"`
	Key            string   `json:"key"`
	Title          string   `json:"title"`
	Objective      string   `json:"objective"`
	Acceptance     string   `json:"acceptance"`
	RequirementIDs []string `json:"requirement_ids"`
	Paths          []string `json:"paths"`
	Dependencies   []string `json:"dependencies"`
	HotResources   []string `json:"hot_resources"`
	BudgetSeconds  int64    `json:"budget_seconds"`
	MaxRunSeconds  int64    `json:"max_run_seconds"`
	MaxRepairs     int      `json:"max_repairs"`
	MaxRetries     int      `json:"max_retries"`
}
type ScopeProposal struct {
	TaskID          string   `json:"task_id"`
	ExpectedVersion int64    `json:"expected_version"`
	Paths           []string `json:"paths"`
	Reason          string   `json:"reason"`
}
type MajorDecisionProposal struct {
	Kind     string   `json:"kind"`
	Evidence string   `json:"evidence"`
	Actions  []string `json:"actions"`
}
type LeaderResult struct {
	Reply          string                  `json:"reply"`
	Ready          bool                    `json:"ready"`
	MissingItems   []string                `json:"missing_items"`
	Clarifications []string                `json:"clarifications"`
	Tasks          []TaskProposal          `json:"tasks"`
	Scopes         []ScopeProposal         `json:"scopes"`
	MajorDecisions []MajorDecisionProposal `json:"major_decisions"`
}
type ProposalTaskMapping struct {
	Key    string `json:"key"`
	TaskID string `json:"task_id"`
}
type PlanningRun struct {
	RequirementChangeID  string                `json:"requirement_change_id,omitempty"`
	RequirementRevisions map[string]int64      `json:"requirement_revisions,omitempty"`
	RequirementResult    *RequirementPlan      `json:"requirement_result,omitempty"`
	BaselineVersion      int64                 `json:"baseline_version"`
	BaselineDigest       string                `json:"baseline_digest"`
	Purpose              string                `json:"purpose,omitempty"` // empty legacy purpose means evaluation
	ConversationID       string                `json:"conversation_id,omitempty"`
	AccountingTarget     string                `json:"accounting_target,omitempty"`
	ScopeRequestID       string                `json:"scope_request_id"`
	ExecutionAdapter     string                `json:"execution_adapter"`
	ProcessID            int                   `json:"process_id"`
	WorkingDirectory     string                `json:"working_directory"`
	TaskMapping          []ProposalTaskMapping `json:"task_mapping"`
	ID                   string                `json:"id"`
	Version              int64                 `json:"version"`
	DocumentVersion      int64                 `json:"document_version"`
	AuthorizationVersion int64                 `json:"authorization_version"`
	AutomationVersion    int64                 `json:"automation_version"`
	Generation           int64                 `json:"generation"`
	State                string                `json:"state"`
	Attempt              int                   `json:"attempt"`
	MaxRetries           int                   `json:"max_retries"`
	GrantSeconds         int64                 `json:"grant_seconds"`
	ElapsedSeconds       int64                 `json:"elapsed_seconds"`
	LeaseUntil           time.Time             `json:"lease_until"`
	Deadline             time.Time             `json:"deadline"`
	StartedAt            time.Time             `json:"started_at"`
	TreeStopped          bool                  `json:"tree_stopped"`
	Settled              bool                  `json:"settled"`
	SessionID            string                `json:"session_id"`
	Detail               string                `json:"detail"`
	PendingResult        *LeaderResult         `json:"pending_result,omitempty"`
	TaskIDs              []string              `json:"task_ids"`
}
type Decision struct {
	ID                   string   `json:"id"`
	Version              int64    `json:"version"`
	SourceKind           string   `json:"source_kind"`
	SourceID             string   `json:"source_id"`
	SourceVersion        int64    `json:"source_version"`
	DocumentVersion      int64    `json:"document_version"`
	AuthorizationVersion int64    `json:"authorization_version"`
	Kind                 string   `json:"kind"`
	Evidence             string   `json:"evidence"`
	Actions              []string `json:"actions"`
	Status               string   `json:"status"`
	Resolution           string   `json:"resolution"`
}
type DecisionResolveRequest struct {
	ExpectedVersion       int64  `json:"expected_version"`
	ExpectedSourceVersion int64  `json:"expected_source_version"`
	Action                string `json:"action"`
	Evidence              string `json:"evidence"`
}
type ToolScheduling struct {
	Tool            string `json:"tool"`
	Enabled         bool   `json:"enabled"`
	Implement       bool   `json:"implement"`
	Review          bool   `json:"review"`
	Model           string `json:"model,omitempty"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}
type NodeConfigureRequest struct {
	ExpectedVersion int64            `json:"expected_version"`
	Name            string           `json:"name"`
	Capacity        int              `json:"capacity"`
	Draining        bool             `json:"draining"`
	DisabledNewWork bool             `json:"disabled_new_work"`
	Tools           []ToolScheduling `json:"tools"`
}
type BlockItem struct {
	Kind     string   `json:"kind"`
	ObjectID string   `json:"object_id"`
	Reason   string   `json:"reason"`
	Actions  []string `json:"actions"`
}

// GoalContext is the authorized context needed by an assigned execution. It
// excludes Leader conversations, decisions, planning runs and owner authority.
type GoalContext struct {
	Version              int64    `json:"version"`
	AuthorizationVersion int64    `json:"authorization_version"`
	Objective            string   `json:"objective"`
	NonGoals             string   `json:"non_goals"`
	Acceptance           string   `json:"acceptance"`
	Architecture         string   `json:"architecture"`
	DirectoryOutline     string   `json:"directory_outline"`
	AllowedRoots         []string `json:"allowed_roots"`
}

// RunScopeRequest is bound to the authenticated Run transport, never an owner grant.
type RunScopeRequest struct {
	Generation int64    `json:"generation"`
	Reason     string   `json:"reason"`
	Paths      []string `json:"paths"`
}
type ScopeAssessment struct {
	ID                       string   `json:"id"`
	Version                  int64    `json:"version"`
	RunID                    string   `json:"run_id"`
	Generation               int64    `json:"generation"`
	TaskID                   string   `json:"task_id"`
	TaskVersion              int64    `json:"task_version"`
	AuthorizationVersion     int64    `json:"authorization_version"`
	GoalVersion              int64    `json:"goal_version"`
	GoalAuthorizationVersion int64    `json:"goal_authorization_version"`
	AutomationVersion        int64    `json:"automation_version"`
	PolicyVersion            int64    `json:"policy_version"`
	Reason                   string   `json:"reason"`
	Paths                    []string `json:"paths"`
	State                    string   `json:"state"`
}
