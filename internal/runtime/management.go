package runtime

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"math"

	"coordica/internal/domain"
	"coordica/internal/store"
)

type grantRecord struct {
	Grant
	TokenHash []byte `json:"token_hash"`
}

// Manager is a trusted host bootstrap/configuration capability, equivalent to
// already holding this Store. It must not be exposed through application,
// legacy Owner/node/chat routes. It does not authenticate an external manager.
type Manager struct{ db *store.Store }

func NewManager(db *store.Store) *Manager { return &Manager{db: db} }

func (m *Manager) ConfigureGrant(ctx context.Context, config GrantConfig, expectedVersion int64) (Grant, error) {
	var result Grant
	if m == nil || m.db == nil || !validGrant(config) || expectedVersion < 0 || expectedVersion == math.MaxInt64 {
		return result, ErrInvalid
	}
	h := sha256.Sum256([]byte(config.Token))
	err := m.db.Update(ctx, func(tx *store.Tx) error {
		var old grantRecord
		err := tx.Get(kindGrant, config.ID, &old)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if err == nil && old.ApplicationID != config.ApplicationID ||
			err == nil && old.Version != expectedVersion || errors.Is(err, domain.ErrNotFound) && expectedVersion != 0 {
			return ErrConflict
		}
		result = config.Grant
		result.Version = expectedVersion + 1
		if err = tx.Put(kindGrant, config.ID, grantRecord{Grant: result, TokenHash: h[:]}); err != nil {
			return err
		}
		return audit(tx, "runtime:management", "grant.configured", config.ID)
	})
	if err != nil {
		return Grant{}, err
	}
	return result, nil
}

func validGrant(config GrantConfig) bool {
	if !identifier(config.ID, 128) || !identifier(config.ApplicationID, 128) || len(config.Token) < 32 ||
		len(config.Token) > 512 || len(config.Actions) > 6 || len(config.Targets) > 64 {
		return false
	}
	actions := map[Action]bool{}
	for _, action := range config.Actions {
		if actions[action] || action != Submit && action != Read && action != WriteContext && action != Register && action != Continue && action != Cancel {
			return false
		}
		actions[action] = true
	}
	targets := map[string]bool{}
	for _, target := range config.Targets {
		key := compound(target.InstanceID, target.NodeID)
		if !identifier(target.InstanceID, 128) || !identifier(target.NodeID, 128) || targets[key] {
			return false
		}
		targets[key] = true
	}
	return true
}

func (m *Manager) ConfigureInstance(ctx context.Context, config Instance, expectedVersion int64) (Instance, error) {
	var result Instance
	if m == nil || m.db == nil || !validInstance(config) || expectedVersion < 0 || expectedVersion == math.MaxInt64 {
		return result, ErrInvalid
	}
	err := m.db.Update(ctx, func(tx *store.Tx) error {
		var old Instance
		err := tx.Get(kindInstance, config.ID, &old)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if err == nil && (old.ApplicationID != config.ApplicationID || old.NodeID != config.NodeID || old.Harness != config.Harness || old.Version != expectedVersion) ||
			errors.Is(err, domain.ErrNotFound) && expectedVersion != 0 {
			return ErrConflict
		}
		result = config
		result.Version = expectedVersion + 1
		if err = tx.Put(kindInstance, config.ID, result); err != nil {
			return err
		}
		return audit(tx, "runtime:management", "instance.configured", config.ID)
	})
	if err != nil {
		return Instance{}, err
	}
	return result, nil
}

func validInstance(instance Instance) bool {
	return identifier(instance.ID, 128) && identifier(instance.ApplicationID, 128) && identifier(instance.NodeID, 128) &&
		identifier(instance.Harness, 128) && identifier(instance.ConfigRef, 512) &&
		(instance.Support == "supported" || instance.Support == "unsupported" || instance.Support == "unknown" || instance.Support == "conditional")
}

func authorize(tx *store.Tx, credential Credential, action Action) (Grant, error) {
	var record grantRecord
	if !identifier(credential.PrincipalID, 128) || len(credential.Token) < 32 || len(credential.Token) > 512 {
		return Grant{}, ErrForbidden
	}
	err := tx.Get(kindGrant, credential.PrincipalID, &record)
	if errors.Is(err, domain.ErrNotFound) {
		return Grant{}, ErrForbidden
	}
	if err != nil {
		return Grant{}, err
	}
	h := sha256.Sum256([]byte(credential.Token))
	if record.ID != credential.PrincipalID || !identifier(record.ApplicationID, 128) || record.Version <= 0 ||
		!record.Enabled || subtle.ConstantTimeCompare(record.TokenHash, h[:]) != 1 {
		return Grant{}, ErrForbidden
	}
	for _, allowed := range record.Actions {
		if allowed == action {
			return record.Grant, nil
		}
	}
	return Grant{}, ErrForbidden
}

func targetFor(tx *store.Tx, grant Grant, target Target, prepare bool) (Instance, error) {
	allowed := false
	for _, scope := range grant.Targets {
		allowed = allowed || scope.InstanceID == target.InstanceID && scope.NodeID == target.NodeID
	}
	if !allowed {
		return Instance{}, ErrForbidden
	}
	var instance Instance
	err := tx.Get(kindInstance, target.InstanceID, &instance)
	if err != nil {
		return Instance{}, err
	}
	if instance.ApplicationID != grant.ApplicationID || instance.NodeID != target.NodeID || instance.Harness != target.Harness {
		return Instance{}, ErrForbidden
	}
	if prepare && (!instance.Enabled || instance.Version != target.InstanceVersion) {
		return Instance{}, ErrConflict
	}
	return instance, nil
}
