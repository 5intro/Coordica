package acpclient

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"
	"unicode/utf8"

	"coordica/internal/jsoninput"
	"coordica/internal/process"
)

const promptTimeout = 5 * time.Minute // Client operation cap, not an ACP/Task limit.
var ErrSessionBusy = errors.New("ACP Session already has an in-flight prompt or configuration call")
var ErrPromptBusy = ErrSessionBusy // Retained for existing Prompt callers.
var ErrInvalidPrompt = errors.New("invalid ACP explicit text prompt")

// PromptResult is a correlated native terminal claim, never output text, a
// business-success judgement, an immediate ACK or a native Turn ID.
type PromptResult struct {
	LocalRequestID uint64
	Evidence       string
	StopReason     string
}

type PromptError struct {
	Cause   error
	Process process.Result
}

func (e *PromptError) Error() string { return "ACP prompt failed: " + e.Cause.Error() }
func (e *PromptError) Unwrap() error { return e.Cause }

type promptReply struct {
	result PromptResult
	err    error
}
type pendingPrompt struct {
	id              uint64
	replies         chan promptReply
	writeDone       chan struct{}
	received, ready bool
	reply           promptReply // protected by Peer.mu, retained through close/select races.
}

// PromptText sends one explicit text input and waits for its full terminal
// response, bounded by the shorter caller deadline or this client's five-minute
// cap. No Task context is fetched and there is no authentication/tool fallback.
func (s Session) PromptText(ctx context.Context, text string) (PromptResult, error) {
	if ctx == nil || text == "" || len(text) > 64<<10 || !utf8.ValidString(text) {
		return PromptResult{}, ErrInvalidPrompt
	}
	if err := ctx.Err(); err != nil {
		return PromptResult{}, err
	}
	if _, err := s.capture(); err != nil {
		return PromptResult{}, err
	}
	p := s.peer
	p.mu.Lock()
	if p.err != nil {
		p.mu.Unlock()
		return PromptResult{}, ErrDisconnected
	}
	if p.prompt != nil || p.configCall != nil {
		p.mu.Unlock()
		return PromptResult{}, ErrPromptBusy
	}
	id := p.lastRequestID + 1
	if id < 3 {
		id = 3
	}
	if p.lastRequestID >= math.MaxInt64 {
		p.mu.Unlock()
		return PromptResult{}, ErrProtocol
	}
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": "session/prompt", "params": map[string]any{"sessionId": s.id, "prompt": []map[string]string{{"type": "text", "text": text}}}})
	if len(raw) > maxFrameBytes {
		p.mu.Unlock()
		return PromptResult{}, ErrInvalidPrompt
	}
	pending := &pendingPrompt{id: id, replies: make(chan promptReply, 1), writeDone: make(chan struct{})}
	p.prompt, p.lastRequestID = pending, id
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		if p.prompt == pending {
			p.prompt = nil
		}
		p.mu.Unlock()
	}()
	go func() {
		defer close(pending.writeDone)
		if err := p.process.Write(append(raw, '\n')); err != nil {
			p.updates.fault("prompt_write_failed")
			p.fail(errors.Join(ErrOutcomeUnknown, err))
		}
	}()
	op, cancel := context.WithTimeout(ctx, promptTimeout)
	defer cancel()
	var reply promptReply
	select {
	case reply = <-pending.replies:
	case <-p.failed:
		reply = p.promptFailure(pending, p.Err())
	case <-op.Done():
		reply = p.promptFailure(pending, op.Err())
	}
	if reply.result.Evidence == "rpc_terminal" || isRPCRejection(reply.err) {
		select {
		case <-pending.writeDone:
			if p.Err() == nil {
				return reply.result, reply.err
			}
		case <-p.failed:
		case <-op.Done():
		}
	}
	p.updates.fault("prompt_connection_failed")
	status, stopErr := p.Close()
	// Decode may have completed while cancellation/EOF selected another arm.
	// Preserve that known terminal/rejection separately from failed transport.
	latest := p.promptFailure(pending, reply.err)
	if latest.result.Evidence == "rpc_terminal" || isRPCRejection(latest.err) {
		return latest.result, errors.Join(latest.err, &PromptError{Cause: ErrDisconnected, Process: status})
	}
	cause := errors.Join(ErrOutcomeUnknown, reply.err)
	if !status.TreeStopped {
		cause = errors.Join(cause, stopErr, process.ErrTreeNotStopped)
	}
	return PromptResult{}, &PromptError{Cause: cause, Process: status}
}

func isRPCRejection(err error) bool { var rpc *RPCError; return errors.As(err, &rpc) }

func (p *Peer) promptFailure(pending *pendingPrompt, cause error) promptReply {
	p.mu.Lock()
	defer p.mu.Unlock()
	if pending.ready {
		return pending.reply
	}
	return promptReply{err: errors.Join(ErrOutcomeUnknown, cause)}
}

func (p *Peer) receivePrompt(id uint64, result, rpc []byte, hasError bool) error {
	p.mu.Lock()
	pending := p.prompt
	if pending == nil || pending.id != id || pending.received {
		p.mu.Unlock()
		return ErrProtocol
	}
	pending.received = true
	p.mu.Unlock()
	var reply promptReply
	if hasError {
		values, err := object(rpc)
		if err != nil || jsoninput.Required(rpc, "code", "message") != nil {
			return ErrProtocol
		}
		var rejection RPCError
		if decode(values, "code", &rejection.Code, true) != nil || decode(values, "message", &rejection.Message, true) != nil {
			return ErrProtocol
		}
		rejection.Data = append(json.RawMessage(nil), values["data"]...)
		reply.err = &rejection
	} else {
		values, err := object(result)
		if err != nil {
			return err
		}
		var reason string
		if decode(values, "stopReason", &reason, true) != nil {
			return ErrProtocol
		}
		switch reason {
		case "end_turn", "max_tokens", "max_turn_requests", "refusal", "cancelled":
		default:
			return ErrProtocol
		}
		reply.result = PromptResult{LocalRequestID: id, Evidence: "rpc_terminal", StopReason: reason}
	}
	p.mu.Lock()
	pending.ready, pending.reply = true, reply
	p.mu.Unlock()
	pending.replies <- reply
	return nil
}
