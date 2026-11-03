package runtime

import (
	"context"
	"errors"
	"time"
)

var ErrUnsupported = errors.New("runtime operation is unsupported for this target")

const kindWorkInterrupt = "runtime.work-interrupt"

type InterruptWorkRequest struct {
	Key             string `json:"key"`
	WorkID          string `json:"work_id"`
	Generation      int64  `json:"work_generation"`
	ExpectedVersion int64  `json:"expected_work_version"`
}

// WorkInterrupt is a control receipt. It does not replace the Work terminal
// state or attest process stop. At most one command can be claimed per Work.
type WorkInterrupt struct {
	ID        string          `json:"id"`
	WorkID    string          `json:"work_id"`
	Work      Work            `json:"work_snapshot"`
	State     string          `json:"state"`
	Observed  NativeInterrupt `json:"observed"`
	ClaimedAt time.Time       `json:"claimed_at"`
}

type WorkInterruptCall struct {
	Target                           NativeCreate
	NativeID                         NativeSessionID
	WorkID, WorkCommandID, CommandID string
	WorkGeneration                   int64
	NativeTurnID, SourceEpoch        string
}

type NativeInterrupt struct {
	Outcome string `json:"outcome"` // acknowledged, rejected, not_sent, unknown.
}

// WorkInterruptPort only controls an already-owned immutable native Turn.
// Unsupported targets cannot fall back to Stop, a new Start or a new Session.
type WorkInterruptPort interface {
	InterruptWork(context.Context, WorkInterruptCall) (NativeInterrupt, error)
}
