package runtime

import (
	"context"
	"errors"
	"math"

	"coordica/internal/store"
)

// Release closes only this producer's known idle binding. Unknown/busy/foreign
// ownership requires reconciliation; a nil port error is not stop evidence.
func (p *BindingProducer) Release(ctx context.Context, credential Credential, request BindingRequest) (Binding, error) {
	if p == nil || !identifier(request.BindingID, 128) || request.ExpectedVersion <= 0 || request.Generation <= 0 {
		return Binding{}, ErrInvalid
	}
	commandID, err := freshID("command")
	if err != nil {
		return Binding{}, err
	}
	var binding Binding
	var run Run
	var claim BindingCommand
	claimed := false
	_, err = mutate(p.ledger, ctx, credential, SessionControl, request.Key, "binding.release_requested", request,
		func(tx *store.Tx, grant Grant) (err error) {
			binding, _, run, err = bindingFor(tx, grant, request.BindingID, false)
			if err == nil && (binding.Config != p.config || binding.PrincipalID != grant.ID) {
				err = ErrForbidden
			}
			return err
		}, func(tx *store.Tx, _ Grant) (Binding, string, bool, error) {
			if binding.State == "released" {
				return binding, binding.ID, false, nil
			}
			if binding.State != "ready" || binding.OwnerID != p.owner {
				return Binding{}, "", false, ErrStopUnconfirmed
			}
			if binding.Version != request.ExpectedVersion || binding.Generation != request.Generation ||
				binding.Version == math.MaxInt64 || run.Version == math.MaxInt64 {
				return Binding{}, "", false, ErrConflict
			}
			claim = BindingCommand{ID: commandID, BindingID: binding.ID, Generation: binding.Generation,
				OwnerID: p.owner, Operation: "release", State: "inflight"}
			binding.Version++
			binding.State, binding.Outcome, binding.CommandID = "releasing", "inflight", commandID
			run.NativeDisposition, run.Version = "session_pending", run.Version+1
			if err := putNew(tx, kindBindingCommand, claim.ID, claim); err != nil {
				return Binding{}, "", false, err
			}
			if err := tx.Put(kindBinding, binding.ID, binding); err != nil {
				return Binding{}, "", false, err
			}
			claimed = true
			return binding, binding.ID, true, tx.Put(kindRun, run.ID, run)
		})
	if err != nil {
		return Binding{}, err
	}
	if !claimed {
		if binding.State == "released" {
			return binding, nil
		}
		return binding, ErrOutcomeUnknown
	}
	call := nativeCall(binding, claim)
	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rpcCleanupTimeout)
	stop, stopErr := p.port.Stop(stopCtx, call, binding.NativeID)
	cancel()
	if stopErr == nil && scopedStop(stop, call, binding.NativeID) {
		finishCtx, done := context.WithTimeout(context.WithoutCancel(ctx), rpcCleanupTimeout)
		result, err := p.finishRelease(finishCtx, credential, binding, claim, stop)
		done()
		if err == nil {
			return result, nil
		}
		stopErr = err
	}
	unknownCtx, done := context.WithTimeout(context.WithoutCancel(ctx), rpcCleanupTimeout)
	defer done()
	err = p.quarantine(unknownCtx, binding, claim, NativeCreation{Outcome: "stop_unconfirmed", ID: binding.NativeID}, scopedStop(stop, call, binding.NativeID), stop)
	if fenceErr := p.releaseDisclosureFence(unknownCtx, credential, binding, claim); fenceErr != nil {
		var safePersistence error
		if err != nil {
			safePersistence = ErrConflict
		}
		return Binding{}, errors.Join(ErrStopUnconfirmed, ErrOutcomeUnknown, fenceErr, safePersistence)
	}
	return Binding{}, errors.Join(ErrStopUnconfirmed, ErrOutcomeUnknown, stopErr, err)
}

// Cleanup authority follows Release admission, not permission to create a new
// resource. A retired instance or newer grant version can still allow cleanup.
func (p *BindingProducer) releaseDisclosureFence(ctx context.Context, credential Credential, original Binding, claim BindingCommand) error {
	return p.ledger.db.View(ctx, func(tx *store.Tx) error {
		grant, err := authorize(tx, credential, SessionControl)
		if err != nil {
			return err
		}
		current, _, _, err := bindingFor(tx, grant, original.ID, false)
		if err != nil {
			return err
		}
		if current.PrincipalID != grant.ID || current.Config != original.Config || current.Config != p.config {
			return ErrForbidden
		}
		if current.OwnerID != claim.OwnerID || current.Generation != claim.Generation || current.CommandID != claim.ID {
			return ErrConflict
		}
		var command BindingCommand
		if err := tx.Get(kindBindingCommand, claim.ID, &command); err != nil {
			return err
		}
		if command.BindingID != original.ID || command.OwnerID != claim.OwnerID || command.Generation != claim.Generation || command.Operation != "release" {
			return ErrConflict
		}
		return nil
	})
}

func (p *BindingProducer) finishRelease(ctx context.Context, credential Credential, original Binding, claim BindingCommand, stop BindingStop) (Binding, error) {
	var result Binding
	err := p.ledger.db.Update(ctx, func(tx *store.Tx) error {
		grant, err := authorize(tx, credential, SessionControl)
		if err != nil {
			return err
		}
		binding, _, run, err := bindingFor(tx, grant, original.ID, false)
		if err != nil || binding.Config != p.config || binding.PrincipalID != grant.ID {
			if err == nil {
				err = ErrForbidden
			}
			return err
		}
		var command BindingCommand
		if err = tx.Get(kindBindingCommand, claim.ID, &command); err != nil {
			return err
		}
		if !sameClaim(binding, command, claim) || binding.Version == math.MaxInt64 || run.Version == math.MaxInt64 {
			return ErrConflict
		}
		binding.State, binding.Outcome, binding.CleanupStopped = "released", "stopped", true
		binding.Version++
		command.State, command.Stop = "completed", stop
		run.NativeDisposition, run.Version = "session_stopped", run.Version+1
		if err = tx.Put(kindBindingCommand, command.ID, command); err == nil {
			err = tx.Put(kindBinding, binding.ID, binding)
		}
		if err == nil {
			err = tx.Put(kindRun, run.ID, run)
		}
		if err == nil {
			err = audit(tx, grant.ID, "binding.released", binding.ID)
		}
		result = binding
		return err
	})
	return result, err
}
