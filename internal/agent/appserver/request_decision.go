package appserver

import (
	"bytes"
	"encoding/json"

	"coordica/internal/jsoninput"
)

// Decide forwards only a decision offered by this exact native callback (or a
// pinned basic schema value when no list was provided). It never synthesizes a
// policy amendment, changes the permission mode or grants Task authority.
func (r Request) Decide(decision json.RawMessage) (ResponseReceipt, error) {
	if r.frame.owner == nil || len(r.decisions) == 0 {
		return ResponseReceipt{}, ErrInvalidAnswer
	}
	canonical, err := canonicalDecision(decision)
	if err != nil {
		return ResponseReceipt{}, err
	}
	for _, offered := range r.decisions {
		candidate, err := canonicalDecision(offered)
		if err == nil && bytes.Equal(canonical, candidate) {
			return r.frame.owner.Respond(r.frame, map[string]any{"decision": canonical})
		}
	}
	return ResponseReceipt{}, ErrInvalidAnswer
}

func canonicalDecision(raw json.RawMessage) (json.RawMessage, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || len(raw) > 16<<10 {
		return nil, ErrInvalidAnswer
	}
	var value any
	if raw[0] == '{' {
		if jsoninput.Decode(raw, &value, 16<<10) != nil {
			return nil, ErrInvalidAnswer
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if decoder.Decode(&value) != nil {
			return nil, ErrInvalidAnswer
		}
	} else if raw[0] == '"' {
		var text string
		if json.Unmarshal(raw, &text) != nil {
			return nil, ErrInvalidAnswer
		}
		value = text
	} else {
		return nil, ErrInvalidAnswer
	}
	result, err := json.Marshal(value)
	if err != nil {
		return nil, ErrInvalidAnswer
	}
	return result, nil
}
