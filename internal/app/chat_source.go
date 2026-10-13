package app

import (
	"encoding/json"
	"errors"

	"coordica/internal/domain"
	"coordica/internal/store"
)

// A transport retry may arrive with a new write key. Source identity prevents
// that duplicate message creating a second central submission. Intent is
// explicit: requirement, consultation and proposal have independent receipts.
type chatSourceReceipt struct {
	Digest   string          `json:"digest"`
	Response json.RawMessage `json:"response"`
}

func chatSourceKey(action string, source domain.ChatSource) string {
	return Hash(action + "\x00" + source.ClientID + "\x00" + source.SessionID + "\x00" + source.MessageID)
}
func chatSourceReplay(tx *store.Tx, action string, source domain.ChatSource, input any) (json.RawMessage, bool, error) {
	var receipt chatSourceReceipt
	e := tx.Get("chat_source_receipt", chatSourceKey(action, source), &receipt)
	if errors.Is(e, domain.ErrNotFound) {
		return nil, false, nil
	}
	if e != nil {
		return nil, false, e
	}
	raw, e := json.Marshal(input)
	if e != nil {
		return nil, false, e
	}
	if receipt.Digest != Hash(string(raw)) {
		return nil, false, domain.Fail("idempotency_conflict", "source message was already submitted with different content")
	}
	return receipt.Response, true, nil
}
func rememberChatSource(tx *store.Tx, action string, source domain.ChatSource, input, result any) error {
	raw, e := json.Marshal(input)
	if e != nil {
		return e
	}
	response, e := json.Marshal(result)
	if e != nil {
		return e
	}
	return tx.Put("chat_source_receipt", chatSourceKey(action, source), chatSourceReceipt{Digest: Hash(string(raw)), Response: response})
}
