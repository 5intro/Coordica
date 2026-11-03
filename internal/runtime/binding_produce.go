package runtime

import (
	"context"
	"errors"
	"math"
	"time"

	"coordica/internal/store"
)

func (p *BindingProducer) claim(ctx context.Context, credential Credential, id string) (Binding, BindingCommand, error) {
	var binding Binding
	var command BindingCommand
	err := p.ledger.db.Update(ctx, func(tx *store.Tx) error {
		grant, err := authorize(tx, credential, SessionControl)
		if err != nil {
			return err
		}
		var task Task
		var run Run
		binding, task, run, err = bindingFor(tx, grant, id, true)
		if err != nil {
			return err
		}
		if err = liveBinding(grant, task, run); err != nil {
			return err
		}
		if binding.Config != p.config || binding.Version == math.MaxInt64 {
			return ErrConflict
		}
		if err = tx.Get(kindBindingCommand, binding.CommandID, &command); err != nil {
			return err
		}
		if binding.State != "creating" || binding.Outcome != "pending" || command.State != "pending" {
			return ErrOutcomeUnknown
		}
		if command.BindingID != binding.ID || command.Generation != binding.Generation || command.Operation != "create" {
			return ErrConflict
		}
		binding.OwnerID, binding.Version, binding.Outcome = p.owner, binding.Version+1, "inflight"
		command.OwnerID, command.State = p.owner, "inflight"
		if err = tx.Put(kindBindingCommand, command.ID, command); err == nil {
			err = tx.Put(kindBinding, binding.ID, binding)
		}
		if err == nil {
			err = audit(tx, grant.ID, "binding.claimed", binding.ID)
		}
		return err
	})
	return binding, command, err
}

func nativeCall(binding Binding, command BindingCommand) NativeCreate {
	return NativeCreate{BindingID: binding.ID, CommandID: command.ID, OwnerID: command.OwnerID,
		Generation: command.Generation, Config: binding.Config}
}

func validNative(id NativeSessionID, harness string) bool {
	return id.Harness == harness && identifier(id.Value, 512)
}

func scopedStop(stop BindingStop, call NativeCreate, id NativeSessionID) bool {
	return stop.TreeStopped && stop.BindingID == call.BindingID && stop.OwnerID == call.OwnerID && stop.NativeID == id
}

// Produce claims one pending create exactly once, then performs the host port
// call outside the database transaction. It never sends work or retries a call.
func (p *BindingProducer) Produce(ctx context.Context, credential Credential, id string) (Binding, error) {
	if p == nil || !identifier(id, 128) {
		return Binding{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return Binding{}, err
	}
	binding, command, err := p.claim(ctx, credential, id)
	if err != nil {
		return Binding{}, err
	}
	return p.perform(ctx, credential, binding, command, p.port.Create)
}

func (p *BindingProducer) perform(ctx context.Context, credential Credential, binding Binding, command BindingCommand,
	invoke func(context.Context, NativeCreate) (NativeCreation, error)) (Binding, error) {
	call := nativeCall(binding, command)
	observation := NativeCreation{Outcome: "not_sent"}
	callErr := ctx.Err()
	if callErr == nil {
		callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		observation, callErr = invoke(callCtx, call)
		cancel()
	}
	confirmed := callErr == nil && observation.Outcome == "confirmed" &&
		validNative(observation.ID, binding.Config.Target.Harness) && observation.Effective == binding.Config
	if command.Operation == "resume" && observation.ID != binding.NativeID {
		confirmed = false
	}
	safeFailure := callErr != nil && observation.ID == (NativeSessionID{}) &&
		(observation.Outcome == "rejected" || observation.Outcome == "not_sent")
	stopped, stopErr := false, error(nil)
	var stopProof BindingStop
	cleanupID := observation.ID
	if command.Operation == "resume" {
		cleanupID = binding.NativeID
	}
	if !confirmed {
		stopped, stopProof, stopErr = p.cleanup(ctx, call, cleanupID)
	}
	if confirmed || safeFailure && stopped && stopErr == nil {
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rpcCleanupTimeout)
		result, err := p.finish(finishCtx, credential, binding, command, observation, confirmed, stopProof)
		cancel()
		if err == nil {
			return result, callErr
		}
		callErr = errors.Join(callErr, err)
	}
	// Quarantine only this owned port. A cleanup stop does not resolve the
	// external create outcome or release the logical binding for another start.
	if confirmed {
		stopped, stopProof, stopErr = p.cleanup(ctx, call, cleanupID)
	}
	unknownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rpcCleanupTimeout)
	defer cancel()
	unknownErr := p.quarantine(unknownCtx, binding, command, observation, stopped, stopProof)
	if fenceErr := p.disclosureFence(unknownCtx, credential, binding, command); fenceErr != nil {
		// Observations stay on the original command. An expired caller receives
		// neither a native RPCError nor a cleanup error carrying private data.
		var safeStop, safePersistence error
		if !stopped || stopErr != nil {
			safeStop = ErrStopUnconfirmed
		}
		if unknownErr != nil {
			safePersistence = ErrConflict
		}
		return Binding{}, errors.Join(ErrOutcomeUnknown, fenceErr, safeStop, safePersistence)
	}
	return Binding{}, errors.Join(ErrOutcomeUnknown, callErr, stopErr, unknownErr)
}

// disclosureFence checks current authority/identity, independently of the
// command's own completed/unknown state transitions and first-write CAS.
func (p *BindingProducer) disclosureFence(ctx context.Context, credential Credential, original Binding, claim BindingCommand) error {
	return p.ledger.db.View(ctx, func(tx *store.Tx) error {
		grant, err := authorize(tx, credential, SessionControl)
		if err != nil {
			return err
		}
		current, task, run, err := bindingFor(tx, grant, original.ID, true)
		if err != nil {
			return err
		}
		if err := liveBinding(grant, task, run); err != nil {
			return err
		}
		if current.Config != p.config || current.OwnerID != claim.OwnerID || current.Generation != claim.Generation || current.CommandID != claim.ID {
			return ErrConflict
		}
		return nil
	})
}

func (p *BindingProducer) cleanup(ctx context.Context, call NativeCreate, id NativeSessionID) (bool, BindingStop, error) {
	if id.Harness != "" && id.Harness != call.Config.Target.Harness {
		id = NativeSessionID{}
	}
	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rpcCleanupTimeout)
	defer cancel()
	stop, err := p.port.Stop(stopCtx, call, id)
	return scopedStop(stop, call, id), stop, err
}
