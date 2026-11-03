package acpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"

	"coordica/internal/jsoninput"
	"coordica/internal/process"
)

// Session pins a native ID to this Peer and its trusted canonical directory.
// Identity alone does not attest liveness, selected model, mode or authority.
type Session struct {
	peer          *Peer
	id, directory string
}

func (s Session) ID() string        { return s.id }
func (s Session) Directory() string { return s.directory }

type sessionReply struct {
	session Session
	err     error
}

// NewSession allows one allocation attempt per Peer, including a known
// rejection. This is a conservative client policy, not an ACP protocol limit.
// It never authenticates, loads history, prompts, or selects model/permissions.
func (p *Peer) NewSession(ctx context.Context) (Session, error) {
	if p == nil || ctx == nil {
		return Session{}, ErrProtocol
	}
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	p.mu.Lock()
	if p.err != nil || p.info.ProtocolVersion != 1 {
		p.mu.Unlock()
		return Session{}, ErrDisconnected
	}
	if p.sessionClaimed {
		p.mu.Unlock()
		return Session{}, ErrSessionAllocated
	}
	p.sessionClaimed, p.sessionPending = true, true
	p.sessionReply, p.sessionWriteDone = make(chan sessionReply, 1), make(chan struct{})
	resultChannel, writeDone := p.sessionReply, p.sessionWriteDone
	p.mu.Unlock()
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "session/new", "params": map[string]any{"cwd": p.directory, "mcpServers": []any{}}})
	go func() {
		defer close(writeDone)
		if err := p.process.Write(append(raw, '\n')); err != nil {
			p.fail(errors.Join(ErrOutcomeUnknown, err))
		}
	}()
	op, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var result sessionReply
	select {
	case result = <-resultChannel:
	case <-p.failed:
		result = p.sessionFailure(resultChannel, p.Err())
	case <-op.Done():
		result = p.sessionFailure(resultChannel, op.Err())
	}
	if result.err != nil {
		var rejection *RPCError
		if errors.As(result.err, &rejection) {
			return Session{}, result.err
		}
	}
	if result.err == nil {
		result.err = p.Err()
	}
	if result.err != nil {
		status, stopErr := p.Close()
		cause := errors.Join(ErrOutcomeUnknown, result.err)
		if !status.TreeStopped {
			cause = errors.Join(cause, stopErr, process.ErrTreeNotStopped)
		}
		p.mu.Lock()
		observed := p.sessionID
		p.mu.Unlock()
		return Session{}, &SessionCreationError{Cause: cause, NativeSessionID: observed, Process: status}
	}
	return result.session, nil
}

func (p *Peer) sessionFailure(replies <-chan sessionReply, cause error) sessionReply {
	select {
	case result := <-replies:
		if result.err != nil {
			return result
		}
	default:
	}
	return sessionReply{err: errors.Join(ErrOutcomeUnknown, cause)}
}

// Load replays history through session/update and is not implemented here,
// even when the agent advertises loadSession. No native request is sent.
func (p *Peer) LoadSession(context.Context, string) (Session, error) {
	return Session{}, ErrUnsupported
}

func (p *Peer) receiveSession(result, rpc []byte, hasError bool) error {
	p.mu.Lock()
	if !p.sessionPending {
		p.mu.Unlock()
		return ErrProtocol
	}
	p.sessionPending = false
	replies := p.sessionReply
	p.mu.Unlock()
	var reply sessionReply
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
		var id string
		if decode(values, "sessionId", &id, true) != nil || id == "" || len(id) > 512 || strings.ContainsFunc(id, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
			return ErrProtocol
		}
		p.mu.Lock()
		p.sessionID = id
		p.mu.Unlock()
		if modes, exists := values["modes"]; exists && !bytes.Equal(bytes.TrimSpace(modes), []byte("null")) {
			if _, err := object(modes); err != nil {
				return err
			}
		}
		if options, exists := values["configOptions"]; exists && !bytes.Equal(bytes.TrimSpace(options), []byte("null")) {
			var array []json.RawMessage
			if json.Unmarshal(options, &array) != nil {
				return ErrProtocol
			}
		}
		// Optional configuration declarations are not retained/interpreted. No
		// selected/effective model, effort or permission is claimed by this ID.
		reply.session = Session{peer: p, id: id, directory: p.directory}
	}
	replies <- reply
	return nil
}
