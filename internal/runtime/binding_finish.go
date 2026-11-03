package runtime

import (
	"context"
	"errors"
	"math"
	"time"

	"coordica/internal/domain"
	"coordica/internal/store"
)

const rpcCleanupTimeout = 5 * time.Second
const kindBindingNative = "runtime.binding-native"

func sameClaim(binding Binding, command BindingCommand, expected BindingCommand) bool {
	return binding.CommandID == expected.ID && binding.Generation == expected.Generation && binding.OwnerID == expected.OwnerID &&
		command.ID == expected.ID && command.Generation == expected.Generation && command.OwnerID == expected.OwnerID && command.State == "inflight"
}

func (p *BindingProducer) finish(ctx context.Context, credential Credential, original Binding, claim BindingCommand,
	observation NativeCreation, confirmed bool, stop BindingStop) (Binding, error) {
	var result Binding
	err := p.ledger.db.Update(ctx, func(tx *store.Tx) error {
		grant, err := authorize(tx, credential, SessionControl)
		if err != nil {
			return err
		}
		binding, task, run, err := bindingFor(tx, grant, original.ID, true)
		if err != nil {
			return err
		}
		if err = liveBinding(grant, task, run); err != nil {
			return err
		}
		var command BindingCommand
		if err = tx.Get(kindBindingCommand, claim.ID, &command); err != nil {
			return err
		}
		if !sameClaim(binding, command, claim) || binding.Config != p.config || binding.Version == math.MaxInt64 || run.Version == math.MaxInt64 {
			return ErrConflict
		}
		stopID := observation.ID
		if claim.Operation == "resume" {
			stopID = binding.NativeID
		}
		if !confirmed && !scopedStop(stop, nativeCall(binding, claim), stopID) {
			return ErrStopUnconfirmed
		}
		binding.Version++
		binding.Outcome = observation.Outcome
		if claim.Operation == "create" {
			binding.NativeID = observation.ID
		}
		command.State, command.Observed = "completed", observation
		if confirmed {
			key := compound(binding.Config.Target.NodeID, observation.ID.Harness, binding.Config.ProfileRef, observation.ID.Value)
			if claim.Operation == "resume" {
				var occupied string
				err = tx.Get(kindBindingNative, key, &occupied)
				if err == nil && (occupied != binding.ID || observation.ID != binding.NativeID) {
					err = ErrConflict
				}
			} else {
				err = putNew(tx, kindBindingNative, key, binding.ID)
			}
			if err != nil {
				return err
			}
			binding.State, run.NativeDisposition = "ready", "session_bound"
		} else {
			command.Stop, binding.CleanupStopped = stop, true
			if claim.Operation == "resume" {
				binding.State, run.NativeDisposition = "released", "session_stopped"
			} else {
				binding.State, run.NativeDisposition = "unbound", "not_started"
			}
		}
		run.Version++
		if err = tx.Put(kindBindingCommand, command.ID, command); err == nil {
			err = tx.Put(kindBinding, binding.ID, binding)
		}
		if err == nil {
			err = tx.Put(kindRun, run.ID, run)
		}
		if err == nil {
			err = audit(tx, grant.ID, "binding.confirmed", binding.ID)
		}
		result = binding
		return err
	})
	return result, err
}

// quarantine records only the original command's observation. A newer binding
// generation/owner is never advanced by this late completion. This is a trusted
// host fact write, not an application authorization or an execution retry.
func (p *BindingProducer) quarantine(ctx context.Context, original Binding, claim BindingCommand, observation NativeCreation, stopped bool, stop BindingStop) error {
	return p.ledger.db.Update(ctx, func(tx *store.Tx) error {
		var binding Binding
		var command BindingCommand
		if err := tx.Get(kindBinding, original.ID, &binding); err != nil {
			return err
		}
		if err := tx.Get(kindBindingCommand, claim.ID, &command); err != nil {
			return err
		}
		if command.BindingID != original.ID || command.OwnerID != claim.OwnerID || command.Generation != claim.Generation {
			return ErrConflict
		}
		command.Observed, command.State, command.Stop = observation, "unknown", stop
		if validNative(observation.ID, original.Config.Target.Harness) {
			key := compound(original.Config.Target.NodeID, observation.ID.Harness, original.Config.ProfileRef, observation.ID.Value)
			var occupied string
			err := tx.Get(kindBindingNative, key, &occupied)
			if errors.Is(err, domain.ErrNotFound) {
				if err = putNew(tx, kindBindingNative, key, original.ID); err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
		}
		if err := tx.Put(kindBindingCommand, command.ID, command); err != nil {
			return err
		}
		currentCommand := binding.CommandID == claim.ID || claim.Operation == "read" && binding.CommandID == original.CommandID
		if binding.Generation == claim.Generation && binding.OwnerID == claim.OwnerID && currentCommand {
			var run Run
			if err := tx.Get(kindRun, original.RunID, &run); err != nil {
				return err
			}
			if binding.Version == math.MaxInt64 || run.Version == math.MaxInt64 {
				return ErrConflict
			}
			binding.State, binding.Outcome = "unknown", "unknown"
			if claim.Operation == "create" {
				binding.NativeID = observation.ID
			}
			binding.CleanupStopped, binding.CommandID = stopped, claim.ID
			binding.Version++
			run.NativeDisposition, run.Version = "session_unknown", run.Version+1
			if err := tx.Put(kindBinding, binding.ID, binding); err != nil {
				return err
			}
			if err := tx.Put(kindRun, run.ID, run); err != nil {
				return err
			}
		}
		return audit(tx, "runtime:host", "binding.quarantined", command.ID)
	})
}
