// Package domain contains transport-neutral records and pure input rules.
// It has no dependency on a database, HTTP server, Git, or an agent SDK.
package domain

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

const APIVersion = "v1"

type Check struct {
	Name           string   `json:"name"`
	Command        string   `json:"command"`
	Args           []string `json:"args"`
	TimeoutSeconds int64    `json:"timeout_seconds"`
}
type Project struct {
	ID                 string    `json:"id"`
	Version            int64     `json:"version"`
	Name               string    `json:"name"`
	RepositoryURL      string    `json:"repository_url"`
	TargetBranch       string    `json:"target_branch"`
	Paused             bool      `json:"paused"`
	TotalBudgetSeconds int64     `json:"total_budget_seconds"`
	UsedSeconds        int64     `json:"used_seconds"`
	ReservedSeconds    int64     `json:"reserved_seconds"`
	ProtectedPaths     []string  `json:"protected_paths"`
	Checks             []Check   `json:"checks"`
	PolicyVersion      int64     `json:"policy_version"`
	CreatedAt          time.Time `json:"created_at"`
}
type Requirement struct {
	ID        string    `json:"id"`
	Version   int64     `json:"version"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}
type Task struct {
	ID                   string    `json:"id"`
	Version              int64     `json:"version"`
	Title                string    `json:"title"`
	Description          string    `json:"description"`
	Acceptance           string    `json:"acceptance"`
	RequirementIDs       []string  `json:"requirement_ids"`
	Dependencies         []string  `json:"dependencies"`
	AllowedPaths         []string  `json:"allowed_paths"`
	HotResources         []string  `json:"hot_resources"`
	Tool                 string    `json:"tool"`
	State                string    `json:"state"`
	HoldReason           string    `json:"hold_reason"`
	ApprovedVersion      int64     `json:"approved_version"`
	AuthorizationVersion int64     `json:"authorization_version"`
	Generation           int64     `json:"generation"`
	BudgetSeconds        int64     `json:"budget_seconds"`
	UsedSeconds          int64     `json:"used_seconds"`
	ReservedSeconds      int64     `json:"reserved_seconds"`
	MaxRunSeconds        int64     `json:"max_run_seconds"`
	MaxRepairs           int       `json:"max_repairs"`
	MaxRetries           int       `json:"max_retries"`
	Repairs              int       `json:"repairs"`
	Retries              int       `json:"retries"`
	CurrentRunID         string    `json:"current_run_id"`
	CurrentCandidateID   string    `json:"current_candidate_id"`
	CreatedAt            time.Time `json:"created_at"`
}
type Capability struct {
	Tool       string `json:"tool"`
	Version    string `json:"version"`
	Available  bool   `json:"available"`
	Structured bool   `json:"structured"`
	Protocol   string `json:"protocol,omitempty"`
	Review     bool   `json:"review"`
	ReadOnly   bool   `json:"read_only"`
	Resume     bool   `json:"resume"`
	Cancel     bool   `json:"cancel"`
	Detail     string `json:"detail"`
}
type Node struct {
	ID           string       `json:"id"`
	Version      int64        `json:"version"`
	Name         string       `json:"name"`
	Platform     string       `json:"platform"`
	Capabilities []Capability `json:"capabilities"`
	Capacity     int          `json:"capacity"`
	Revoked      bool         `json:"revoked"`
	LastSeen     time.Time    `json:"last_seen"`
	TokenHash    string       `json:"-"`
}
type Run struct {
	ID                   string     `json:"id"`
	Version              int64      `json:"version"`
	TaskID               string     `json:"task_id"`
	NodeID               string     `json:"node_id"`
	Kind                 string     `json:"kind"`
	Tool                 string     `json:"tool"`
	State                string     `json:"state"`
	Generation           int64      `json:"generation"`
	AuthorizationVersion int64      `json:"authorization_version"`
	PolicyVersion        int64      `json:"policy_version"`
	LeaseUntil           time.Time  `json:"lease_until"`
	Deadline             time.Time  `json:"deadline"`
	GrantSeconds         int64      `json:"grant_seconds"`
	ElapsedSeconds       int64      `json:"elapsed_seconds"`
	Base                 string     `json:"base"`
	Head                 string     `json:"head"`
	CandidateID          string     `json:"candidate_id"`
	SessionID            string     `json:"session_id"`
	Detail               string     `json:"detail"`
	Quiescent            bool       `json:"quiescent"`
	StartedAt            time.Time  `json:"started_at"`
	FinishedAt           *time.Time `json:"finished_at,omitempty"`
}
type Review struct {
	Candidate string   `json:"candidate"`
	Verdict   string   `json:"verdict"`
	Summary   string   `json:"summary"`
	Findings  []string `json:"findings"`
	RunID     string   `json:"run_id"`
}
type CheckResult struct {
	Name            string `json:"name"`
	Passed          bool   `json:"passed"`
	ExitCode        int    `json:"exit_code"`
	Output          string `json:"output"`
	DurationSeconds int64  `json:"duration_seconds"`
}
type Candidate struct {
	ID                   string        `json:"id"`
	Version              int64         `json:"version"`
	TaskID               string        `json:"task_id"`
	SourceRunID          string        `json:"source_run_id"`
	AuthorizationVersion int64         `json:"authorization_version"`
	PolicyVersion        int64         `json:"policy_version"`
	Base                 string        `json:"base"`
	Head                 string        `json:"head"`
	Main                 string        `json:"main"`
	Commit               string        `json:"commit"`
	State                string        `json:"state"`
	Detail               string        `json:"detail"`
	Protected            bool          `json:"protected"`
	ApprovedCommit       string        `json:"approved_commit"`
	Review               *Review       `json:"review,omitempty"`
	Checks               []CheckResult `json:"checks"`
	CreatedAt            time.Time     `json:"created_at"`
}
type Event struct {
	Sequence int64     `json:"sequence"`
	Time     time.Time `json:"time"`
	Actor    string    `json:"actor"`
	Kind     string    `json:"kind"`
	ObjectID string    `json:"object_id"`
	Detail   string    `json:"detail"`
}
type Snapshot struct {
	APIVersion   string        `json:"api_version"`
	Project      *Project      `json:"project"`
	Requirements []Requirement `json:"requirements"`
	Tasks        []Task        `json:"tasks"`
	Runs         []Run         `json:"runs"`
	Candidates   []Candidate   `json:"candidates"`
	Nodes        []Node        `json:"nodes"`
	Events       []Event       `json:"events"`
}
type Pairing struct {
	ID        string    `json:"id"`
	CodeHash  string    `json:"code_hash"`
	Name      string    `json:"name"`
	ExpiresAt time.Time `json:"expires_at"`
	Used      bool      `json:"used"`
}
type Credential struct {
	ID     string `json:"id"`
	Hash   string `json:"hash"`
	Role   string `json:"role"`
	NodeID string `json:"node_id"`
}
type Resource struct {
	ID     string `json:"id"`
	TaskID string `json:"task_id"`
}
type Intent struct {
	ID          string    `json:"id"`
	CandidateID string    `json:"candidate_id"`
	Expected    string    `json:"expected"`
	Desired     string    `json:"desired"`
	State       string    `json:"state"`
	Detail      string    `json:"detail"`
	CreatedAt   time.Time `json:"created_at"`
}
type Artifact struct {
	ID        string    `json:"id"`
	RunID     string    `json:"run_id"`
	SHA256    string    `json:"sha256"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
}

type VersionRequest struct {
	ExpectedVersion int64 `json:"expected_version"`
}
type ScopeRequest struct {
	ExpectedVersion int64    `json:"expected_version"`
	AllowedPaths    []string `json:"allowed_paths"`
	Reason          string   `json:"reason"`
}
type RegisterRequest struct {
	Code         string       `json:"code"`
	Name         string       `json:"name"`
	Platform     string       `json:"platform"`
	Capacity     int          `json:"capacity"`
	Capabilities []Capability `json:"capabilities"`
}
type RegisterResponse struct {
	NodeID string `json:"node_id"`
	Token  string `json:"token"`
}
type ClaimRequest struct {
	Capabilities []Capability `json:"capabilities"`
}
type Assignment struct {
	Run              Run        `json:"run"`
	Task             Task       `json:"task"`
	Project          Project    `json:"project"`
	Candidate        *Candidate `json:"candidate,omitempty"`
	InputURL         string     `json:"input_url"`
	HeartbeatSeconds int        `json:"heartbeat_seconds"`
}
type HeartbeatRequest struct {
	Generation     int64  `json:"generation"`
	ElapsedSeconds int64  `json:"elapsed_seconds"`
	Detail         string `json:"detail"`
}
type HeartbeatResponse struct {
	Cancel     bool      `json:"cancel"`
	Reason     string    `json:"reason"`
	LeaseUntil time.Time `json:"lease_until"`
}
type CompleteRequest struct {
	Generation     int64   `json:"generation"`
	ElapsedSeconds int64   `json:"elapsed_seconds"`
	Status         string  `json:"status"`
	Head           string  `json:"head"`
	ArtifactID     string  `json:"artifact_id"`
	SessionID      string  `json:"session_id"`
	Detail         string  `json:"detail"`
	Quiescent      bool    `json:"quiescent"`
	Review         *Review `json:"review,omitempty"`
}
type Problem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

var ErrNotFound = errors.New("not found")

type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string        { return e.Message }
func Fail(code, message string) error { return &Error{code, message} }
func ValidateProject(p Project) error {
	if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.RepositoryURL) == "" {
		return Fail("invalid_input", "项目名称和仓库地址不能为空")
	}
	if p.TotalBudgetSeconds <= 0 {
		return Fail("invalid_input", "必须设置有限的项目总预算")
	}
	if !filepath.IsAbs(p.RepositoryURL) {
		u, e := url.Parse(p.RepositoryURL)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return Fail("invalid_input", "仓库需要不含凭据的HTTPS地址或中心本机绝对路径")
		}
	}
	if p.TargetBranch == "" || strings.HasPrefix(p.TargetBranch, "-") || strings.ContainsAny(p.TargetBranch, " \t\n~^:?*[\\") || strings.Contains(p.TargetBranch, "..") {
		return Fail("invalid_input", "目标分支无效")
	}
	if len(p.Checks) == 0 {
		return Fail("invalid_input", "至少需要一个明确的最低检查，空门禁不能放行")
	}
	for _, c := range p.Checks {
		if c.Name == "" || c.Command == "" || c.TimeoutSeconds <= 0 || c.TimeoutSeconds > 3600 {
			return Fail("invalid_input", "检查必须包含名称、可执行文件和不超过一小时的超时")
		}
	}
	return nil
}
func ValidateTask(t Task) error {
	if strings.TrimSpace(t.Title) == "" || strings.TrimSpace(t.Description) == "" || strings.TrimSpace(t.Acceptance) == "" || len(t.AllowedPaths) == 0 {
		return Fail("invalid_input", "任务必须包含标题、目标、验收和允许路径")
	}
	if err := ValidateToolID(t.Tool); err != nil {
		return err
	}
	if t.BudgetSeconds <= 0 || t.BudgetSeconds > 14400 || t.MaxRunSeconds <= 0 || t.MaxRunSeconds > 3600 || t.MaxRunSeconds > t.BudgetSeconds || t.MaxRepairs < 0 || t.MaxRepairs > 2 || t.MaxRetries < 0 || t.MaxRetries > 2 {
		return Fail("invalid_input", "无效的任务预算或次数限制")
	}
	for _, p := range t.AllowedPaths {
		if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "\\") || strings.Contains(p, "..") {
			return Fail("invalid_input", fmt.Sprintf("无效路径模式: %s", p))
		}
	}
	return nil
}

type StopConfirmation struct {
	ExpectedVersion int64  `json:"expected_version"`
	Evidence        string `json:"evidence"`
}
type BudgetRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	BudgetSeconds   int64  `json:"budget_seconds"`
	MaxRepairs      int    `json:"max_repairs"`
	MaxRetries      int    `json:"max_retries"`
	Reason          string `json:"reason"`
}

// ValidateToolID validates the protocol identifier, not a product whitelist.
// Registration and availability are machine-local facts reported by a Runner.
// Task.Tool uses the canonical capability ID; launch aliases are local metadata.
func ValidateToolID(id string) error {
	if len(id) == 0 || len(id) > 64 || id[0] < 'a' || id[0] > 'z' {
		return Fail("invalid_input", "工具 ID 须以小写字母开头，最多 64 个 ASCII 字符")
	}
	for _, c := range id {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '_' {
			return Fail("invalid_input", "工具 ID 仅支持小写字母、数字、连字符和下划线")
		}
	}
	return nil
}

// ValidateCapabilities limits node-supplied metadata before it reaches state
// or scheduling. False and omitted capabilities never grant execution powers.
func ValidateCapabilities(caps []Capability) error {
	if len(caps) > 64 {
		return Fail("invalid_input", "节点最多可报告 64 个工具")
	}
	seen := make(map[string]bool, len(caps))
	for _, c := range caps {
		if err := ValidateToolID(c.Tool); err != nil {
			return err
		}
		if seen[c.Tool] {
			return Fail("invalid_input", "节点工具 ID 不可重复")
		}
		seen[c.Tool] = true
		if len(c.Version) > 1024 || len(c.Detail) > 4096 || len(c.Protocol) > 64 {
			return Fail("invalid_input", "节点能力元数据超过长度限制")
		}
		if c.Protocol != "" {
			if err := ValidateToolID(c.Protocol); err != nil {
				return err
			}
		}
	}
	return nil
}
