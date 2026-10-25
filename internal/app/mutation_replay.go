package app

import (
	"context"
	"encoding/json"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func mutationDigest(key, action string, input any) (string, error) {
	if key == "" || len(key) > 200 {
		return "", domain.Fail("invalid_input", "写操作需要 Idempotency-Key")
	}
	data, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	return Hash(action + "\x00" + string(data)), nil
}

// A use case must check its audience first. This read only preserves an
// accepted receipt before expensive I/O; mutate rechecks it atomically later.
func readMutationReplay(ctx context.Context, s *Service, actor Actor, key, action string, input any) (json.RawMessage, bool, error) {
	digest, err := mutationDigest(key, action, input)
	if err != nil {
		return nil, false, err
	}
	var raw json.RawMessage
	var found bool
	err = s.DB.View(ctx, func(tx *store.Tx) error {
		var err error
		raw, found, err = tx.Replay(actor.ID, key, digest)
		return err
	})
	return raw, found, err
}
