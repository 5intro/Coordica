package acpclient

import (
	"encoding/json"
	"errors"
)

func (p *Peer) claimPermissionLocked(stored *pendingPermission, outcome, optionID string) {
	stored.state, stored.done = outcome, make(chan struct{})
	selection := map[string]any{"outcome": outcome}
	if outcome == "selected" {
		for _, option := range stored.request.options {
			if option.ID == optionID {
				selection["optionId"] = json.RawMessage(option.rawID)
				break
			}
		}
	}
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(stored.request.id), "result": map[string]any{"outcome": selection}})
	go p.writePermission(stored, append(raw, '\n'))
}

func (p *Peer) writePermission(stored *pendingPermission, raw []byte) {
	defer close(stored.done)
	<-stored.prompt.writeDone
	if stored.state == "cancelled" {
		<-stored.prompt.cancelSent
	}
	p.mu.Lock()
	valid := p.err == nil && p.prompt == stored.prompt
	if stored.state == "selected" {
		valid = valid && !stored.prompt.received
	} else {
		valid = valid && stored.prompt.cancelAttempted
	}
	if !valid {
		stored.err = ErrStalePermission
		if p.err == nil && !stored.attempted {
			stored.prompt.permissionUnresolved = true
		}
		p.releasePermissionLocked(stored)
		p.mu.Unlock()
		return
	}
	stored.attempted = true
	p.mu.Unlock()
	err := p.process.Write(raw)
	p.mu.Lock()
	stored.wrote, stored.err = err == nil, err
	p.releasePermissionLocked(stored)
	p.mu.Unlock()
	if err != nil {
		p.updates.fault("permission_write_failed")
		p.fail(errors.Join(ErrOutcomeUnknown, err))
	}
}

// Caller cancellation and selected claims linearize under Peer.mu. The cancel
// writer joins previously claimed selections before it emits session/cancel.
func (p *Peer) cancelPermissionsLocked(pending *pendingPrompt) []<-chan struct{} {
	var selected []<-chan struct{}
	for _, stored := range p.permissions.requests {
		if stored.prompt != pending {
			continue
		}
		if stored.state == "pending" && stored.done == nil {
			p.claimPermissionLocked(stored, "cancelled", "")
		} else if stored.state == "selected" && stored.done != nil {
			selected = append(selected, stored.done)
		}
	}
	return selected
}

func (p *Peer) invalidatePermissionsLocked(pending *pendingPrompt) {
	for _, stored := range p.permissions.requests {
		if pending != nil && stored.prompt != pending {
			continue
		}
		if stored.done == nil && stored.state == "pending" {
			stored.state = "stale"
			stored.prompt.permissionUnresolved = true
			p.releasePermissionLocked(stored)
		}
	}
}

func (p *Peer) permissionWritersLocked(pending *pendingPrompt) []<-chan struct{} {
	var writers []<-chan struct{}
	for _, stored := range p.permissions.requests {
		if stored.done != nil && (pending == nil || stored.prompt == pending) {
			writers = append(writers, stored.done)
		}
	}
	return writers
}

func (p *Peer) retirePermissionScopeLocked(pending *pendingPrompt) {
	for _, stored := range p.permissions.requests {
		if stored.prompt == pending {
			// Writers were joined before this scope can retire. Keep only the
			// bounded identity tombstone, not an old Prompt/rejection/channel.
			stored.prompt = nil
			stored.request.session = Session{}
		}
	}
}
