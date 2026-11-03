package runtime

import (
	"context"
	"math"

	"coordica/internal/store"
)

// ResumeBinding restores only a released, proven-stopped native context. An
// unknown or still-owned resource cannot be taken over through this operation.
func (p *BindingProducer) ResumeBinding(ctx context.Context, credential Credential, request BindingRequest) (Binding, error) {
	if p == nil || !identifier(request.BindingID, 128) || request.ExpectedVersion <= 0 || request.Generation <= 0 {
		return Binding{}, ErrInvalid
	}
	port, ok := p.port.(SessionPort)
	if !ok {
		return Binding{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return Binding{}, err
	}
	id, err := freshID("command")
	if err != nil {
		return Binding{}, err
	}
	var binding Binding
	var run Run
	var claim BindingCommand
	claimed := false
	_, err = mutate(p.ledger, ctx, credential, SessionControl, request.Key, "binding.resume_requested", request,
		func(tx *store.Tx, grant Grant) (err error) {
			var task Task
			binding, task, run, err = bindingFor(tx, grant, request.BindingID, true)
			if err == nil {
				err = liveBinding(grant, task, run)
			}
			if err == nil && binding.Config != p.config {
				err = ErrForbidden
			}
			return err
		}, func(tx *store.Tx, _ Grant) (Binding, string, bool, error) {
			if binding.State != "released" || !binding.CleanupStopped || run.NativeDisposition != "session_stopped" ||
				!validNative(binding.NativeID, binding.Config.Target.Harness) {
				return Binding{}, "", false, ErrStopUnconfirmed
			}
			if binding.Version != request.ExpectedVersion || binding.Generation != request.Generation ||
				binding.Version == math.MaxInt64 || binding.Generation == math.MaxInt64 || run.Version == math.MaxInt64 {
				return Binding{}, "", false, ErrConflict
			}
			var previous BindingCommand
			if err := tx.Get(kindBindingCommand, binding.CommandID, &previous); err != nil {
				return Binding{}, "", false, err
			}
			if !scopedStop(previous.Stop, nativeCall(binding, previous), binding.NativeID) {
				return Binding{}, "", false, ErrStopUnconfirmed
			}
			binding.Version++
			binding.Generation++
			binding.State, binding.Outcome, binding.OwnerID = "resuming", "inflight", p.owner
			binding.CommandID, binding.CleanupStopped = id, false
			claim = BindingCommand{ID: id, BindingID: binding.ID, Generation: binding.Generation,
				OwnerID: p.owner, Operation: "resume", State: "inflight"}
			if err := putNew(tx, kindBindingCommand, id, claim); err != nil {
				return Binding{}, "", false, err
			}
			run.NativeDisposition, run.Version = "session_pending", run.Version+1
			if err := tx.Put(kindRun, run.ID, run); err != nil {
				return Binding{}, "", false, err
			}
			claimed = true
			return binding, binding.ID, true, tx.Put(kindBinding, binding.ID, binding)
		})
	if err != nil {
		return Binding{}, err
	}
	if !claimed {
		return binding, ErrOutcomeUnknown
	}
	return p.perform(ctx, credential, binding, claim, func(ctx context.Context, call NativeCreate) (NativeCreation, error) {
		return port.Resume(ctx, call, binding.NativeID)
	})
}
