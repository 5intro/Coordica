package runtimecodex

import (
	"context"

	"coordica/internal/runtime"
)

// StartWork forwards exactly one explicit text operation on the owned Thread.
// It does not choose a context, model, effort, tool, cwd or permission fallback.
func (p *Port) StartWork(ctx context.Context, call runtime.WorkCall) (runtime.NativeWork, error) {
	base := runtime.NativeWork{Outcome: "not_sent"}
	capture, err := p.capture(call.Target, call.NativeID)
	if err != nil {
		return base, err
	}
	capture.mu.Lock()
	base.SourceEpoch = capture.call.CommandID
	base.AfterSourceSequence = capture.last
	capture.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return base, err
	}
	if call.Effort != call.Target.Config.Target.Effort || call.WorkID == "" || call.CommandID == "" {
		return base, runtime.ErrInvalid
	}
	l, err := p.owned(ctx, call.Target, call.NativeID)
	if err != nil {
		return base, err
	}
	defer l.mu.Unlock()
	// The boundary is taken after serializing with other native operations.
	capture.mu.Lock()
	base.AfterSourceSequence = capture.last
	capture.mu.Unlock()
	turn, err := l.thread.StartText(ctx, call.Text, call.Effort)
	if err != nil {
		base.Outcome = classify(err)
		return base, err
	}
	base.Outcome, base.Evidence, base.State = "accepted", "rpc_accepted", "running"
	base.NativeTurnID = turn.ID()
	if turn.InitialStatus() != "inProgress" {
		base.Outcome, base.Evidence, base.State = "ended", "rpc_terminal", turn.InitialStatus()
	}
	return base, nil
}
