package acpclient

import (
	"context"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"coordica/internal/process"
)

const permissionRequestLimit = 64 // Connection-local tombstones; never a native limit.
const permissionPayloadLimit = 64 << 10

type pendingPermission struct {
	request          PermissionRequest
	key, state       string
	prompt           *pendingPrompt
	bytes            int
	done             chan struct{}
	attempted, wrote bool
	err              error
}

type permissionInbox struct {
	requests []*pendingPermission
	ids      map[string]bool
	bytes    int
}

// PermissionRequests returns only currently selectable private callbacks.
// This local receive window is not native Prompt correlation or Task authority.
func (s Session) PermissionRequests() ([]PermissionRequest, error) {
	if _, err := s.capture(); err != nil {
		return nil, err
	}
	p := s.peer
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return nil, ErrDisconnected
	}
	result := []PermissionRequest{}
	for _, stored := range p.permissions.requests {
		if stored.state != "pending" || stored.done != nil || p.prompt != stored.prompt || stored.prompt.received || stored.prompt.cancelWriteDone != nil {
			continue
		}
		r := stored.request
		r.id, r.toolCall, r.options = r.ID(), r.ToolCall(), r.Options()
		result = append(result, r)
	}
	return result, nil
}

func (p *Peer) receivePermission(fields map[string]json.RawMessage) error {
	p.mu.Lock()
	active := p.sessionReady && p.prompt != nil && !p.prompt.received && p.err == nil
	p.mu.Unlock()
	if !active {
		p.updates.fault("permission_outside_prompt")
		return ErrUnsupportedUpdate
	}
	value, err := parsePermission(fields)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if value.sessionID != p.sessionID || p.prompt == nil || p.prompt.received || p.err != nil {
		p.updates.fault("permission_session_mismatch")
		return ErrProtocol
	}
	if p.permissions.ids[value.key] {
		p.updates.fault("permission_id_reuse")
		return ErrUnsupportedPermission // Reuse remains outside this conservative subset.
	}
	if len(p.permissions.requests) >= permissionRequestLimit || p.permissions.bytes+value.bytes > permissionPayloadLimit {
		p.updates.fault("permission_backpressure")
		return ErrBackpressure
	}
	if p.permissions.ids == nil {
		p.permissions.ids = map[string]bool{}
	}
	serial := uint64(len(p.permissions.requests) + 1)
	stored := &pendingPermission{key: value.key, prompt: p.prompt, state: "pending", bytes: value.bytes,
		request: PermissionRequest{session: Session{peer: p, id: p.sessionID, directory: p.directory, epoch: p.updates.epoch},
			serial: serial, id: value.id, toolCall: value.toolCall, options: value.options}}
	p.permissions.ids[value.key] = true
	p.permissions.requests = append(p.permissions.requests, stored)
	p.permissions.bytes += value.bytes
	if p.prompt.cancelWriteDone != nil {
		p.claimPermissionLocked(stored, "cancelled", "")
	}
	return nil
}

// SelectPermission sends only an option advertised by this exact callback.
// It grants no application authority and reports local writing, never native ACK.
func (s Session) SelectPermission(ctx context.Context, request PermissionRequest, optionID string) (PermissionReceipt, error) {
	if ctx == nil || !utf8.ValidString(optionID) || len(optionID) > 512 {
		return PermissionReceipt{}, ErrInvalidPermissionAnswer
	}
	if err := ctx.Err(); err != nil {
		return PermissionReceipt{}, err
	}
	if request.session.peer != s.peer || request.session.id != s.id || request.session.epoch != s.epoch || request.session.directory != s.directory || request.serial == 0 {
		return PermissionReceipt{}, ErrStalePermission
	}
	if _, err := s.capture(); err != nil {
		return PermissionReceipt{}, err
	}
	p := s.peer
	p.mu.Lock()
	stored, err := s.selectPermissionLocked(request, optionID)
	p.mu.Unlock()
	if err != nil {
		return PermissionReceipt{}, err
	}
	op, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	select {
	case <-stored.done:
		p.mu.Lock()
		written, writeErr, peerErr := stored.wrote, stored.err, p.err
		p.mu.Unlock()
		if peerErr == nil {
			return permissionReceipt(stored, written), writeErr
		}
		err = peerErr
	case <-p.failed:
		err = p.Err()
	case <-op.Done():
		err = op.Err()
	}
	status, stopErr := p.Close()
	p.mu.Lock()
	written := stored.wrote
	p.mu.Unlock()
	cause := errors.Join(err, ErrDisconnected)
	if !written {
		cause = errors.Join(cause, ErrOutcomeUnknown)
	}
	if !status.TreeStopped {
		cause = errors.Join(cause, stopErr, process.ErrTreeNotStopped)
	}
	return permissionReceipt(stored, written), &PermissionReplyError{Cause: cause, Process: status}
}

func (s Session) selectPermissionLocked(request PermissionRequest, optionID string) (*pendingPermission, error) {
	p := s.peer
	if request.serial > uint64(len(p.permissions.requests)) || p.err != nil {
		return nil, ErrStalePermission
	}
	stored := p.permissions.requests[request.serial-1]
	if stored.state != "pending" || stored.done != nil || p.prompt != stored.prompt || stored.prompt.received {
		return nil, ErrStalePermission
	}
	offered := false
	for _, option := range stored.request.options {
		offered = offered || option.ID == optionID
	}
	if !offered {
		return nil, ErrInvalidPermissionAnswer
	}
	// Consuming a queued caller signal here must start the same once-only cancel
	// worker, so it cannot steal the signal from Prompt's waiting goroutine.
	select {
	case <-stored.prompt.cancellation:
		s.beginPromptCancelLocked(stored.prompt)
	default:
	}
	if stored.prompt.cancelWriteDone != nil {
		return nil, ErrStalePermission
	}
	p.claimPermissionLocked(stored, "selected", optionID)
	return stored, nil
}

func permissionReceipt(stored *pendingPermission, written bool) PermissionReceipt {
	if !written {
		return PermissionReceipt{}
	}
	return PermissionReceipt{LocalRequest: stored.request.serial, Evidence: "local_write", Outcome: "selected", Written: true}
}

func (p *Peer) releasePermissionLocked(stored *pendingPermission) {
	p.permissions.bytes -= stored.bytes
	stored.bytes = 0
	stored.request.id, stored.request.toolCall, stored.request.options = nil, nil, nil
}
