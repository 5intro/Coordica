package appserver

import (
	"encoding/json"

	"coordica/internal/jsoninput"
)

type requestScope struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
}

func nativeScope(raw json.RawMessage) requestScope {
	var scope requestScope
	json.Unmarshal(raw, &scope)
	return scope
}

// expireRequests runs before publication, so a slow event consumer cannot
// answer a callback already resolved by the native process. Resolution is not
// approval. A Turn completion alone is not a background-work stop receipt.
// This is connection-local fencing, not durable PendingRequest reconciliation.
func (p *Peer) expireRequests(frame Frame) error {
	switch frame.Method {
	case "serverRequest/resolved":
		var value struct {
			ThreadID  string          `json:"threadId"`
			RequestID json.RawMessage `json:"requestId"`
		}
		if jsoninput.Required(frame.Params, "threadId", "requestId") != nil || json.Unmarshal(frame.Params, &value) != nil || !validNativeID(value.ThreadID) {
			return ErrProtocol
		}
		key, err := idKey(value.RequestID)
		if err != nil {
			return err
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		if stored, exists := p.native[key]; exists {
			if stored.threadID != value.ThreadID {
				return ErrProtocol
			}
			stored.resolved = true
			p.native[key] = stored
		} else {
			// Keep the closure even when its callback was not observed. A
			// delayed/replayed request cannot reuse an already-resolved ID.
			if len(p.native) >= maxNativeIDs {
				return ErrBackpressure
			}
			p.native[key] = nativeRequest{threadID: value.ThreadID, resolved: true}
		}
	}
	return nil
}

// InvalidateRequests closes this Turn's local answer channel after the calling
// control layer ends/revokes its execution authority. It does not send a native
// decision, cancel work or claim anything stopped. Even late callbacks for this
// target remain unanswerable; tombstones are bounded per connection.
func (t Turn) InvalidateRequests() error {
	if !t.valid() {
		return ErrInvalidTarget
	}
	p := t.thread.peer
	scope := requestScope{ThreadID: t.thread.id, TurnID: t.id}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return ErrDisconnected
	}
	if !p.invalidScopes[scope] && len(p.invalidScopes) >= maxNativeIDs {
		return ErrBackpressure
	}
	p.invalidScopes[scope] = true
	for key, stored := range p.native {
		if stored.threadID == scope.ThreadID && stored.turnID == scope.TurnID {
			stored.resolved = true
			p.native[key] = stored
		}
	}
	return nil
}

func (p *Peer) answerable(frame Frame) bool {
	key, err := idKey(frame.ID)
	if err != nil || frame.owner != p {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	stored, exists := p.native[key]
	return exists && !stored.replied && !stored.resolved && stored.digest == fingerprint(frame) && p.err == nil
}
