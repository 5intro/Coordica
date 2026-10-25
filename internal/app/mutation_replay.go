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

func readMutationReplay(ctx context.Context, s *Service, a Actor, key, action string, input any) (json.RawMessage, bool, error) {
	return replayMutation(ctx, s, a, key, action, input, nil)
}

// Replay before external discovery, retaining the same authorization check and
// digest as mutateWithCheck. The final write still rechecks both atomically.
func replayMutation(ctx context.Context, s *Service, a Actor, key, action string, input any,
	check func(*store.Tx) error,
) (json.RawMessage, bool, error) {
	digest, err := mutationDigest(key, action, input)
	if err != nil {
		return nil, false, err
	}
	var result json.RawMessage
	var found bool
	err = s.DB.View(ctx, func(tx *store.Tx) error {
		if check != nil {
			if err := check(tx); err != nil {
				return err
			}
		}
		var err error
		result, found, err = tx.Replay(a.ID, key, digest)
		return err
	})
	return result, found, err
}
