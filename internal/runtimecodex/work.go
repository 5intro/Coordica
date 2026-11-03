package runtimecodex

import (
	"context"

	"coordica/internal/agent/appserver"
	"coordica/internal/runtime"
)

type nativeWorkTarget struct {
	workID, commandID string
	generation        int64
	turn              appserver.Turn
}

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
	l.work = &nativeWorkTarget{workID: call.WorkID, commandID: call.CommandID, generation: call.WorkGeneration, turn: turn}
	base.Outcome, base.Evidence, base.State = "accepted", "rpc_accepted", "running"
	base.NativeTurnID = turn.ID()
	if turn.InitialStatus() != "inProgress" {
		base.Outcome, base.Evidence, base.State = "ended", "rpc_terminal", turn.InitialStatus()
	}
	return base, nil
}

func (p *Port) InterruptWork(ctx context.Context, call runtime.WorkInterruptCall) (runtime.NativeInterrupt, error) {
	result := runtime.NativeInterrupt{Outcome: "not_sent"}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	l, err := p.owned(ctx, call.Target, call.NativeID)
	if err != nil {
		return result, err
	}
	defer l.mu.Unlock()
	if l.work == nil || l.work.workID != call.WorkID || l.work.commandID != call.WorkCommandID || l.work.generation != call.WorkGeneration ||
		l.work.turn.ID() != call.NativeTurnID || l.capture.call.CommandID != call.SourceEpoch || call.CommandID == "" {
		return result, runtime.ErrConflict
	}
	err = l.work.turn.Interrupt(ctx)
	result.Outcome = classify(err)
	if err == nil {
		result.Outcome = "acknowledged"
	}
	return result, err
}
