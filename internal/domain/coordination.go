package domain

import "time"

const MaxWorkReferences = 16
const MaxWorkContextBytes = 256 << 10

// JSON escaping may expand a valid 16 KiB original to six times its size.
const MaxWorkInputBytes = 256 << 10

type WorkAnchor struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// References identify observed versions, never authority to act on an object.
type WorkReference struct {
	Kind                 string `json:"kind"`
	ID                   string `json:"id"`
	Version              int64  `json:"version"`
	AuthorizationVersion int64  `json:"authorization_version,omitempty"`
	Generation           int64  `json:"generation,omitempty"`
	Revision             int64  `json:"revision,omitempty"`
	Stamp                string `json:"stamp"`
}

type WorkNoteInput struct {
	Anchor           WorkAnchor      `json:"anchor"`
	ExpectedStamp    string          `json:"expected_stamp"`
	Kind             string          `json:"kind"`
	OriginalText     string          `json:"original_text"`
	AssistantSummary string          `json:"assistant_summary,omitempty"`
	References       []WorkReference `json:"references"`
	Corrects         string          `json:"corrects,omitempty"`
	Source           ChatSource      `json:"source"`
}

type WorkNote struct {
	OriginalTextSHA256 string          `json:"original_text_sha256"`
	Redacted           bool            `json:"redacted"`
	ID                 string          `json:"id"`
	Sequence           int64           `json:"sequence"`
	ProjectID          string          `json:"project_id"`
	AuthorID           string          `json:"author_id"`
	ChatScope          string          `json:"chat_scope"`
	NodeID             string          `json:"node_id,omitempty"`
	Anchor             WorkAnchor      `json:"anchor"`
	AnchorStamp        string          `json:"anchor_stamp"`
	Kind               string          `json:"kind"`
	OriginalText       string          `json:"original_text"`
	AssistantSummary   string          `json:"assistant_summary,omitempty"`
	References         []WorkReference `json:"references"`
	Corrects           string          `json:"corrects,omitempty"`
	Source             ChatSource      `json:"source"`
	CreatedAt          time.Time       `json:"created_at"`
	Historical         bool            `json:"historical"`
}

type WorkNoteReceipt struct {
	Note        WorkNote `json:"note"`
	Disposition string   `json:"disposition"` // recorded_only, never sent/approved
}

type WorkContextQuery struct {
	Anchor          WorkAnchor      `json:"anchor"`
	Cursor          string          `json:"cursor,omitempty"`
	ReferenceCursor string          `json:"reference_cursor,omitempty"`
	Limit           int             `json:"limit"`
	References      []WorkReference `json:"references,omitempty"`
}

type WorkListQuery struct {
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit"`
	Kind   string `json:"kind,omitempty"`
}

type WorkQuestion struct {
	ID         string `json:"id"`
	Text       string `json:"text"`
	State      string `json:"state"`
	NextAction string `json:"next_action"`
}

type WorkFact struct {
	Reference  WorkReference  `json:"reference"`
	Available  bool           `json:"available"`
	Stale      bool           `json:"stale"`
	Title      string         `json:"title"`
	State      string         `json:"state"`
	Text       string         `json:"text"`
	Truncated  bool           `json:"truncated"`
	Questions  []WorkQuestion `json:"questions"`
	NextAction string         `json:"next_action"`
}

type WorkContext struct {
	QueriedAt           time.Time  `json:"queried_at"`
	Anchor              WorkAnchor `json:"anchor"`
	Current             WorkFact   `json:"current"`
	Notes               []WorkNote `json:"notes"`
	References          []WorkFact `json:"references"`
	NextCursor          string     `json:"next_cursor"`
	NextReferenceCursor string     `json:"next_reference_cursor"`
}

type WorkContextSummary struct {
	Anchor  WorkAnchor `json:"anchor"`
	Current WorkFact   `json:"current"`
	Latest  WorkNote   `json:"latest"`
}

type WorkContextPage struct {
	QueriedAt  time.Time            `json:"queried_at"`
	Items      []WorkContextSummary `json:"items"`
	NextCursor string               `json:"next_cursor"`
}
