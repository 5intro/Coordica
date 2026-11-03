package runtime

import (
	"context"
	"math"

	"coordica/internal/store"
)

// RecoverUnknown is an explicit fence, not native recovery or a takeover. It
// advances the binding generation, preserves command identity/observations and
// makes old producer completion unable to install a ready binding.
func (p *BindingProducer) RecoverUnknown(ctx context.Context, credential Credential, request BindingRequest) (Binding, error) {
	if p == nil || !identifier(request.BindingID, 128) || request.ExpectedVersion <= 0 || request.Generation <= 0 {
		return Binding{}, ErrInvalid
	}
	var binding Binding
	var run Run
	return mutate(p.ledger, ctx, credential, SessionControl, request.Key, "binding.recovery_fenced", request,
		func(tx *store.Tx, grant Grant) (err error) {
			binding, _, run, err = bindingFor(tx, grant, request.BindingID, false)
			if err == nil && (binding.Config != p.config || binding.PrincipalID != grant.ID) {
				err = ErrForbidden
			}
			return err
		}, func(tx *store.Tx, _ Grant) (Binding, string, bool, error) {
			if binding.Version != request.ExpectedVersion || binding.Generation != request.Generation ||
				binding.Version == math.MaxInt64 || binding.Generation == math.MaxInt64 || run.Version == math.MaxInt64 {
				return Binding{}, "", false, ErrConflict
			}
			if binding.State != "creating" && binding.State != "resuming" && binding.State != "ready" && binding.State != "busy" &&
				binding.State != "unknown" && binding.State != "releasing" {
				return Binding{}, "", false, ErrConflict
			}
			var command BindingCommand
			if err := tx.Get(kindBindingCommand, binding.CommandID, &command); err != nil {
				return Binding{}, "", false, err
			}
			binding.Version++
			binding.Generation++
			binding.OwnerID, binding.State, binding.Outcome = "", "unknown", "recovery_required"
			command.State = "unknown"
			run.NativeDisposition, run.Version = "session_unknown", run.Version+1
			if err := tx.Put(kindBindingCommand, command.ID, command); err != nil {
				return Binding{}, "", false, err
			}
			if err := tx.Put(kindRun, run.ID, run); err != nil {
				return Binding{}, "", false, err
			}
			return binding, binding.ID, true, tx.Put(kindBinding, binding.ID, binding)
		})
}
