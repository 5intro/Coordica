package runtime

import (
	"context"
	"errors"
	"time"
)

var ErrWorkBusy = errors.New("runtime binding already has an active work turn")

const (
	kindWork            = "runtime.work"
	kindWorkInput       = "runtime.work-input"
	kindWorkSlot        = "runtime.work-slot"
	kindWorkNative      = "runtime.work-native"
	kindWorkObservation = "runtime.work-observation"
)

type PrepareWorkRequest struct {
	Key             string `json:"key"`
	BindingID       string `json:"binding_id"`
	StreamID        string `json:"stream_id"`
	Generation      int64  `json:"binding_generation"`
	ExpectedVersion int64  `json:"expected_binding_version"`
	Text            string `json:"text"`
}
type Work struct {
	ID         string      `json:"id"`
	CommandID  string      `json:"command_id"`
	Generation int64       `json:"work_generation"`
	Binding    Binding     `json:"binding"` // Immutable execution snapshot, never caller authentication.
	StreamID   string      `json:"stream_id"`
	Source     SourceStamp `json:"source"`
	InputID    string      `json:"input_id"`
	Version    int64       `json:"version"`
	State      string      `json:"state"`
	Observed   NativeWork  `json:"observed"`
	Stop       BindingStop `json:"stop"`
	CreatedAt  time.Time   `json:"created_at"`
	ClaimedAt  time.Time   `json:"claimed_at"`
}
type workInput struct {
	WorkID string `json:"work_id"`
	Text   string `json:"text"`
}
type workSlot struct {
	WorkID     string `json:"work_id"`
	Generation int64  `json:"generation"`
}

// WorkCall is an explicit bounded text operation, not Task context or authority
// to choose a tool/config. The trusted adapter uses the already owned Session.
type WorkCall struct {
	Target                          NativeCreate
	NativeID                        NativeSessionID
	WorkID, CommandID, Text, Effort string
	WorkGeneration                  int64
}
type NativeWork struct {
	Outcome             string `json:"outcome"`                  // accepted, ended, rejected, not_sent, unknown.
	NativeTurnID        string `json:"native_turn_id,omitempty"` // Optional: ACP prompt need not expose one.
	State               string `json:"state,omitempty"`
	Evidence            string `json:"evidence"` // rpc_accepted or rpc_terminal, not local queue acceptance.
	SourceEpoch         string `json:"source_epoch"`
	AfterSourceSequence int64  `json:"after_source_sequence"`
}
type WorkPort interface {
	StartWork(context.Context, WorkCall) (NativeWork, error)
}
