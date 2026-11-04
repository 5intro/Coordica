package appserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"coordica/internal/jsoninput"
)

// RPCError preserves a structured native rejection. Error deliberately omits
// native text/data: those may contain private workspace or credential material.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("app-server RPC error %d", e.Code) }

type nativeRequest struct {
	digest   [32]byte
	replied  bool
	resolved bool
	threadID string
	turnID   string
}

type envelope struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// ResponseReceipt reports only bounded local queue acceptance. JSON-RPC replies
// have no generic native ACK; callers must not claim an approval was consumed.
type ResponseReceipt struct {
	RequestID   json.RawMessage `json:"request_id"`
	Disposition string          `json:"disposition"`
}

func (p *Peer) Respond(request Frame, result any) (ResponseReceipt, error) {
	return p.respond(request, map[string]any{"id": request.ID, "result": result})
}

func (p *Peer) Reject(request Frame, code int, message string) (ResponseReceipt, error) {
	if len(message) > 1024 {
		return ResponseReceipt{}, ErrProtocol
	}
	return p.respond(request, map[string]any{"id": request.ID, "error": RPCError{Code: code, Message: message}})
}

func (p *Peer) respond(request Frame, response any) (ResponseReceipt, error) {
	key, err := idKey(request.ID)
	if err != nil || request.owner != p {
		return ResponseReceipt{}, ErrStaleRequest
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	stored, exists := p.native[key]
	if !exists || stored.replied || stored.resolved || stored.digest != fingerprint(request) || p.err != nil {
		return ResponseReceipt{}, ErrStaleRequest
	}
	if err := p.enqueueLocked(response); err != nil {
		return ResponseReceipt{}, err
	}
	stored.replied = true
	p.native[key] = stored
	return ResponseReceipt{RequestID: append(json.RawMessage{}, request.ID...), Disposition: "queued"}, nil
}

func (p *Peer) receive(raw []byte) error {
	var value envelope
	if !utf8.Valid(raw) || jsoninput.DecodeNullable(raw, &value, maxFrameBytes) != nil || value.JSONRPC != "" && value.JSONRPC != "2.0" {
		return ErrProtocol
	}
	var fields map[string]json.RawMessage
	json.Unmarshal(raw, &fields)
	if fields["jsonrpc"] != nil && value.JSONRPC != "2.0" || fields["method"] != nil && value.Method == "" {
		return ErrProtocol
	}
	if value.Method != "" {
		if !validMethod(value.Method) || fields["result"] != nil || fields["error"] != nil {
			return ErrProtocol
		}
		frame := Frame{Method: value.Method, Params: value.Params, ID: value.ID, owner: p}
		if value.ID == nil {
			if err := p.expireRequests(frame); err != nil {
				return err
			}
		}
		if value.ID != nil {
			key, err := idKey(value.ID)
			if err != nil {
				return err
			}
			p.mu.Lock()
			stored, exists := p.native[key]
			if exists {
				p.mu.Unlock()
				if !stored.replied && stored.digest == fingerprint(frame) {
					return nil
				}
				return ErrProtocol
			}
			if len(p.native) >= maxNativeIDs {
				p.mu.Unlock()
				return ErrBackpressure
			}
			target := nativeScope(frame.Params)
			p.native[key] = nativeRequest{digest: fingerprint(frame), threadID: target.ThreadID, turnID: target.TurnID,
				resolved: p.invalidScopes[target]}
			p.mu.Unlock()
		}
		select {
		case p.events <- frame:
			return nil
		default:
			return ErrBackpressure
		}
	}
	if value.Params != nil || value.Result == nil && value.Error == nil || value.Result != nil && value.Error != nil {
		return ErrProtocol
	}
	if fields["error"] != nil && (value.Error == nil || jsoninput.Required(fields["error"], "code", "message") != nil) {
		return ErrProtocol
	}
	key, err := idKey(value.ID)
	if err != nil {
		return err
	}
	p.mu.Lock()
	waiter, exists := p.waiters[key]
	delete(p.waiters, key)
	p.mu.Unlock()
	if !exists {
		return ErrProtocol
	}
	waiter <- response{result: value.Result, err: value.Error}
	return nil
}

func validMethod(method string) bool {
	return method != "" && len(method) <= 128 && utf8.ValidString(method) && !strings.ContainsFunc(method, unicode.IsSpace) &&
		!strings.ContainsFunc(method, unicode.IsControl)
}

func idKey(raw json.RawMessage) (string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || len(raw) > 512 {
		return "", ErrProtocol
	}
	if raw[0] == '"' {
		var id string
		if json.Unmarshal(raw, &id) == nil {
			return "s:" + id, nil
		}
	} else if _, err := strconv.ParseInt(string(raw), 10, 64); err == nil {
		return "n:" + string(raw), nil
	}
	return "", ErrProtocol
}

func fingerprint(frame Frame) [32]byte {
	raw, _ := json.Marshal(frame)
	return sha256.Sum256(raw)
}
