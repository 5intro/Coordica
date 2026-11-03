package runtime

import (
	"context"
	"errors"
	"time"
)

var (
	ErrCursorExpired     = errors.New("runtime event cursor expired; open a cursor for the current stream")
	ErrEventBackpressure = errors.New("runtime event capacity exhausted; source gap requires reconciliation")
)

const (
	kindEventDataset    = "runtime.event-dataset"
	kindEventStream     = "runtime.event-stream"
	kindEventStreamSlot = "runtime.event-stream-slot"
	kindEventCursor     = "runtime.event-cursor"
	maxStreamEvents     = 256
	maxDatasetEvents    = 10000
)

type SourceStamp struct {
	Epoch    string `json:"epoch"`
	Evidence string `json:"evidence"` // native_sequence or local_observed; never inferred.
}

type EventStream struct {
	ID                       string          `json:"id"`
	DatasetEpoch             string          `json:"dataset_epoch"`
	Source                   SourceStamp     `json:"source"`
	ApplicationID            string          `json:"application_id"`
	TaskID                   string          `json:"task_id"`
	RunID                    string          `json:"run_id"`
	BindingID                string          `json:"binding_id"`
	OwnerID                  string          `json:"owner_id"`
	Generation               int64           `json:"binding_generation"`
	NativeID                 NativeSessionID `json:"native_id"`
	Count                    int64           `json:"count"`
	LastSequence             int64           `json:"last_sequence"`
	LastSourceSequence       int64           `json:"last_source_sequence"`
	ContiguousSourceSequence int64           `json:"contiguous_source_sequence"`
	ObservedThrough          int64           `json:"observed_through"`
	Stop                     BindingStop     `json:"stop"`
	Gap                      bool            `json:"gap"` // Sticky: filling/ACKing later rows does not erase an observed gap.
	State                    string          `json:"state"`
	EndReason                string          `json:"end_reason,omitempty"`
}

// EventFact intentionally has no text, raw params, arbitrary map or history.
type EventFact struct {
	NativeTurnID    string `json:"native_turn_id,omitempty"`
	NativeRequestID string `json:"native_request_id,omitempty"`
	State           string `json:"state,omitempty"`
	Bytes           int64  `json:"bytes,omitempty"`
	Tokens          int64  `json:"tokens,omitempty"`
}
type EventInput struct {
	EventID        string          `json:"event_id"`
	SourceSequence int64           `json:"source_sequence"`
	NativeID       NativeSessionID `json:"native_id"`
	Kind           string          `json:"kind"`
	OccurredAt     time.Time       `json:"occurred_at"`
	Fact           EventFact       `json:"fact"`
}
type RuntimeEvent struct {
	SchemaVersion int        `json:"schema_version"`
	Sequence      int64      `json:"sequence"`
	StreamID      string     `json:"stream_id"`
	ApplicationID string     `json:"application_id"`
	TaskID        string     `json:"task_id"`
	RunID         string     `json:"run_id"`
	BindingID     string     `json:"binding_id"`
	Generation    int64      `json:"binding_generation"`
	Input         EventInput `json:"input"`
	ReceivedAt    time.Time  `json:"received_at"`
}
type EventReceipt struct {
	Sequence       int64 `json:"sequence"`
	SourceSequence int64 `json:"source_sequence"`
	Duplicate      bool  `json:"duplicate"`
}
type EventCursor struct {
	ID            string `json:"id"`
	ApplicationID string `json:"application_id"`
	PrincipalID   string `json:"principal_id"`
	StreamID      string `json:"stream_id"`
	DatasetEpoch  string `json:"dataset_epoch"`
	SourceEpoch   string `json:"source_epoch"`
	Acked         int64  `json:"acked"`
	Offered       int64  `json:"offered"`
}
type EventPage struct {
	Cursor EventCursor    `json:"cursor"`
	Stream EventStream    `json:"stream"`
	Events []RuntimeEvent `json:"events"`
	More   bool           `json:"more"`
}
type SourceEnd struct {
	Reason       string
	LastObserved int64
	Gap          bool
	Stop         BindingStop
}

// EventPort captures bounded normalized observations independently of DB/UI.
// ReleaseCaptured acknowledges only this local buffer, never native delivery.
type EventPort interface {
	Captured(context.Context, NativeCreate, NativeSessionID, int) (SourceStamp, []EventInput, *SourceEnd, error)
	ReleaseCaptured(context.Context, NativeCreate, NativeSessionID, int64) error
}
