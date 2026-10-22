package domain

import "time"

const (
	LeaderActivityVersion = 1
	MaxLeaderEvents       = 4096
	MaxLeaderEventText    = 2048
)

type LeaderExecutionRef struct {
	Kind         string `json:"kind"`
	ID           string `json:"id"`
	Generation   int64  `json:"generation"`
	InputVersion int64  `json:"input_version"`
}

type LeaderExecution struct {
	Ref         LeaderExecutionRef `json:"ref"`
	Version     int64              `json:"version"`
	Purpose     string             `json:"purpose"`
	SourceID    string             `json:"source_id"`
	SourceKind  string             `json:"source_kind,omitempty"`
	State       string             `json:"state"`
	TreeStopped bool               `json:"tree_stopped"`
	Settled     bool               `json:"settled"`
}

// Display-only evidence: no message or tool event grants authority or proves
// completion. Execution state always comes from the center's existing records.
type LeaderActivityEvent struct {
	Sequence  int64              `json:"sequence"`
	Execution LeaderExecutionRef `json:"execution"`
	Key       string             `json:"key"`
	Kind      string             `json:"kind"`
	Text      string             `json:"text"`
	Time      time.Time          `json:"time"`
}

type LeaderActivityQuery struct {
	After      int64  `json:"after"`
	Limit      int    `json:"limit"`
	Kind       string `json:"kind"`
	ID         string `json:"id"`
	Generation int64  `json:"generation"`
}

type LeaderActivityPage struct {
	Version    int                   `json:"version"`
	Events     []LeaderActivityEvent `json:"events"`
	Executions []LeaderExecution     `json:"executions"`
	Cursor     int64                 `json:"cursor"`
	Oldest     int64                 `json:"oldest"`
	Truncated  bool                  `json:"truncated"`
	HasMore    bool                  `json:"has_more"`
	QueriedAt  time.Time             `json:"queried_at"`
}

type LeaderActivitySignal struct{ Key, Kind, Text string }

func ValidateLeaderActivityQuery(q LeaderActivityQuery) error {
	if q.After < 0 || q.After > 9007199254740991 || q.Limit < 1 || q.Limit > 100 || len(q.ID) > 128 || q.Generation < 0 {
		return Fail("invalid_input", "bounded activity cursor and limit 1–100 required")
	}
	if q.Kind != "" && q.Kind != "planning" && q.Kind != "initialization" || (q.Kind == "") != (q.ID == "") || q.Generation > 0 && q.ID == "" {
		return Fail("invalid_input", "activity selection requires both execution kind and ID")
	}
	return nil
}
