package acpclient

import (
	"context"
	"encoding/json"
	"errors"

	"coordica/internal/process"
)

// ErrCancelUnconfirmed preserves another native terminal/rejection after a
// cancellation write may have been emitted. The peer is retired before reuse.
var ErrCancelUnconfirmed = errors.New("ACP prompt did not confirm native cancellation; peer retired")

func (s Session) waitPromptReply(ctx context.Context, pending *pendingPrompt, cancellation <-chan struct{}) promptReply {
	p := s.peer
	for {
		select {
		case reply := <-pending.replies:
			return reply
		case <-p.failed:
			return p.promptFailure(pending, p.Err())
		case <-ctx.Done():
			return p.promptFailure(pending, ctx.Err())
		case <-cancellation:
			cancellation = nil // One notification attempt, with no detached signal watcher.
			s.beginPromptCancel(pending)
		}
	}
}

func (s Session) beginPromptCancel(pending *pendingPrompt) {
	p := s.peer
	p.mu.Lock()
	if p.err != nil || p.prompt != pending || pending.received || pending.cancelWriteDone != nil {
		p.mu.Unlock()
		return
	}
	done := make(chan struct{})
	pending.cancelWriteDone = done
	p.mu.Unlock()
	go func() {
		defer close(done)
		// No cancel frame may precede the original full prompt frame.
		<-pending.writeDone
		p.mu.Lock()
		valid := p.err == nil && p.prompt == pending && !pending.received &&
			p.sessionID == s.id && p.directory == s.directory && p.updates.epoch == s.epoch
		if valid {
			pending.cancelAttempted = true
		}
		p.mu.Unlock()
		if !valid {
			return
		}
		raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "session/cancel", "params": map[string]string{"sessionId": s.id}})
		if err := p.process.Write(append(raw, '\n')); err != nil {
			p.updates.fault("cancel_write_failed")
			p.fail(errors.Join(ErrOutcomeUnknown, err))
		}
	}()
}

func (p *Peer) waitPromptWriters(ctx context.Context, pending *pendingPrompt) bool {
	p.mu.Lock()
	cancelWriter := pending.cancelWriteDone
	p.mu.Unlock()
	for _, done := range []<-chan struct{}{pending.writeDone, cancelWriter} {
		if done == nil {
			continue
		}
		select {
		case <-done:
		case <-p.failed:
			return false
		case <-ctx.Done():
			return false
		}
	}
	return true
}

func (p *Peer) cancelWasAttempted(pending *pendingPrompt) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return pending.cancelAttempted
}

func (p *Peer) unconfirmedPromptCancel(reply promptReply) (PromptResult, error) {
	status, stopErr := p.Close()
	cause := errors.Join(ErrCancelUnconfirmed, ErrDisconnected)
	if !status.TreeStopped {
		cause = errors.Join(cause, stopErr, process.ErrTreeNotStopped)
	}
	return reply.result, errors.Join(reply.err, &PromptError{Cause: cause, Process: status})
}
