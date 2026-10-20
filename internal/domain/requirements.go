package domain

import "time"

// RequirementContext carries only the linked, pinned content contract. It
// excludes chat provenance, private summaries and unrelated requirements.
type RequirementContext struct {
	ID              string `json:"id"`
	ContentRevision int64  `json:"content_revision"`
	State           string `json:"state"`
	Title           string `json:"title"`
	Body            string `json:"body"`
}

// ContentRevision changes with the requirement contract, independently of
// Version, which also covers relationship and metadata edits.
func RequirementRevisionNumber(r Requirement) int64 {
	if r.ContentRevision < 1 {
		return 1
	}
	return r.ContentRevision
}

func RequirementActive(r Requirement) bool { return r.State == "" || r.State == "active" }

type RequirementEdit struct {
	ExpectedVersion int64  `json:"expected_version"`
	Title           string `json:"title,omitempty"`
	Body            string `json:"body,omitempty"`
	Reason          string `json:"reason"`
}

type RequirementRevision struct {
	Requirement Requirement `json:"requirement"`
	ActorID     string      `json:"actor_id"`
	Reason      string      `json:"reason"`
	CreatedAt   time.Time   `json:"created_at"`
}

type RequirementImpact struct {
	Task            Task `json:"task"`
	Shared          bool `json:"shared"`
	NeedsStop       bool `json:"needs_stop"`
	Integrated      bool `json:"integrated"`
	PublicationBusy bool `json:"publication_busy"`
	Direct          bool `json:"direct"`
}

type RequirementChange struct {
	ID                       string              `json:"id"`
	Version                  int64               `json:"version"`
	RequirementID            string              `json:"requirement_id"`
	ContentRevision          int64               `json:"content_revision"`
	Kind                     string              `json:"kind"`
	State                    string              `json:"state"`
	Reason                   string              `json:"reason"`
	GoalVersion              int64               `json:"goal_version"`
	GoalAuthorizationVersion int64               `json:"goal_authorization_version"`
	AutomationVersion        int64               `json:"automation_version"`
	Impacts                  []RequirementImpact `json:"impacts"`
	TaskIDs                  []string            `json:"task_ids"`
	Detail                   string              `json:"detail"`
	CreatedAt                time.Time           `json:"created_at"`
}

type RequirementDetail struct {
	Requirement Requirement           `json:"requirement"`
	Revisions   []RequirementRevision `json:"revisions"`
	Changes     []RequirementChange   `json:"changes"`
	Impact      []RequirementImpact   `json:"impact"`
	Before      int64                 `json:"before"`
}

type RequirementTaskUpdate struct {
	TaskID          string       `json:"task_id"`
	ExpectedVersion int64        `json:"expected_version"`
	Proposal        TaskProposal `json:"proposal"`
}

type RequirementNewTask struct {
	Proposal       TaskProposal `json:"proposal"`
	RelatedTaskIDs []string     `json:"related_task_ids"`
}

type RequirementPlan struct {
	Reply          string                  `json:"reply"`
	Ready          bool                    `json:"ready"`
	Updates        []RequirementTaskUpdate `json:"updates"`
	Tasks          []RequirementNewTask    `json:"tasks"`
	MajorDecisions []MajorDecisionProposal `json:"major_decisions"`
}
