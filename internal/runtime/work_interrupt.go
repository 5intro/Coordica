package runtime

import (
	"context"
	"errors"
	"time"

	"coordica/internal/store"
)

func (p *BindingProducer) interruptTarget(tx *store.Tx, credential Credential, id string) (Grant, Binding, Run, Work, error) {
	g, err := authorize(tx, credential, InterruptTurn)
	var w Work
	if err == nil {
		err = tx.Get(kindWork, id, &w)
	}
	var b Binding
	var r Run
	if err == nil {
		var task Task
		b, task, r, err = bindingFor(tx, g, w.Binding.ID, true)
		if err == nil {
			err = liveBinding(g, task, r)
		}
	}
	if err == nil && (!workIdentityMatches(w, b) || b.OwnerID != p.owner || b.Config != p.config) {
		err = ErrConflict
	}
	return g, b, r, w, err
}

func interruptActive(tx *store.Tx, b Binding, w Work) error {
	if !workMatches(w, b) || b.State != "busy" || w.State != "native_accepted" {
		return ErrConflict
	}
	var slot workSlot
	if err := tx.Get(kindWorkSlot, b.ID, &slot); err != nil {
		return err
	}
	if slot.WorkID != w.ID || slot.Generation != w.Generation {
		return ErrConflict
	}
	var source EventStream
	if err := tx.Get(kindEventStream, w.StreamID, &source); err != nil {
		return err
	}
	if source.State != "open" || source.Source != w.Source || source.BindingID != b.ID || source.OwnerID != b.OwnerID || source.Generation != b.Generation {
		return ErrConflict
	}
	return nil
}

func (p *BindingProducer) InterruptWork(ctx context.Context, credential Credential, request InterruptWorkRequest) (WorkInterrupt, error) {
	if p == nil || !identifier(request.WorkID, 128) || request.Generation <= 0 || request.ExpectedVersion <= 0 {
		return WorkInterrupt{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return WorkInterrupt{}, err
	}
	port, supported := p.port.(WorkInterruptPort)
	id, err := freshID("interrupt")
	if err != nil {
		return WorkInterrupt{}, err
	}
	var b Binding
	var w Work
	claim, err := mutate(p.ledger, ctx, credential, InterruptTurn, request.Key, "work.interrupt_claimed", request,
		func(tx *store.Tx, _ Grant) (err error) {
			_, b, _, w, err = p.interruptTarget(tx, credential, request.WorkID)
			return err
		}, func(tx *store.Tx, _ Grant) (WorkInterrupt, string, bool, error) {
			if !supported || w.Observed.NativeTurnID == "" {
				return WorkInterrupt{}, "", false, ErrUnsupported
			}
			if w.Generation != request.Generation || w.Version != request.ExpectedVersion {
				return WorkInterrupt{}, "", false, ErrConflict
			}
			if err := interruptActive(tx, b, w); err != nil {
				return WorkInterrupt{}, "", false, err
			}
			command := WorkInterrupt{ID: id, WorkID: w.ID, Work: w, State: "claimed", ClaimedAt: time.Now().UTC()}
			err := putNew(tx, kindWorkInterrupt, w.ID, command)
			return command, id, true, err
		})
	if err != nil {
		return WorkInterrupt{}, err
	}
	if claim.ID != id {
		var current WorkInterrupt
		err := p.ledger.db.View(ctx, func(tx *store.Tx) error {
			if _, _, _, _, err := p.interruptTarget(tx, credential, claim.WorkID); err != nil {
				return err
			}
			return tx.Get(kindWorkInterrupt, claim.WorkID, &current)
		})
		return current, err
	}
	call := WorkInterruptCall{Target: nativeCall(w.Binding, BindingCommand{ID: w.Binding.CommandID, OwnerID: w.Binding.OwnerID, Generation: w.Binding.Generation}), NativeID: w.Binding.NativeID,
		WorkID: w.ID, WorkCommandID: w.CommandID, WorkGeneration: w.Generation, CommandID: id, NativeTurnID: w.Observed.NativeTurnID, SourceEpoch: w.Source.Epoch}
	invokeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	observed, nativeErr := port.InterruptWork(invokeCtx, call)
	cancel()
	known := nativeErr == nil && observed.Outcome == "acknowledged" || nativeErr != nil && (observed.Outcome == "rejected" || observed.Outcome == "not_sent")
	if !known {
		observed.Outcome = "unknown"
	}
	finishCtx, done := context.WithTimeout(context.WithoutCancel(ctx), rpcCleanupTimeout)
	defer done()
	result, finishErr := p.finishInterrupt(finishCtx, credential, claim, observed, known)
	if finishErr != nil {
		_, _ = p.finishInterrupt(finishCtx, credential, claim, observed, false)
	}
	fenceErr := p.ledger.db.View(finishCtx, func(tx *store.Tx) error { _, _, _, _, err := p.interruptTarget(tx, credential, w.ID); return err })
	if fenceErr != nil {
		return WorkInterrupt{}, errors.Join(ErrOutcomeUnknown, fenceErr)
	}
	if !known || finishErr != nil {
		return result, errors.Join(ErrOutcomeUnknown, nativeErr, finishErr)
	}
	return result, nativeErr
}

func (p *BindingProducer) finishInterrupt(ctx context.Context, credential Credential, original WorkInterrupt, observed NativeInterrupt, known bool) (WorkInterrupt, error) {
	var result WorkInterrupt
	err := p.ledger.db.Update(ctx, func(tx *store.Tx) error {
		if err := tx.Get(kindWorkInterrupt, original.WorkID, &result); err != nil {
			return err
		}
		if result.ID != original.ID || result.State != "claimed" {
			return ErrConflict
		}
		g, b, r, w, authority := p.interruptTarget(tx, credential, original.WorkID)
		if authority != nil && known {
			return authority
		}
		result.State, result.Observed = "delivery_unknown", observed
		if known {
			result.State = observed.Outcome
		} else if authority == nil && interruptActive(tx, b, w) == nil {
			if err := markSourceUnknown(tx, b, r, false); err != nil {
				return err
			}
		}
		if err := tx.Put(kindWorkInterrupt, result.WorkID, result); err != nil {
			return err
		}
		actor := "runtime:host"
		if authority == nil {
			actor = g.ID
		}
		return audit(tx, actor, "work.interrupt_observed", result.ID)
	})
	if err != nil {
		return WorkInterrupt{}, err
	}
	return result, nil
}

func (l *Ledger) ReadWorkInterrupt(ctx context.Context, credential Credential, workID string) (WorkInterrupt, error) {
	if _, err := l.ReadWork(ctx, credential, workID); err != nil {
		return WorkInterrupt{}, err
	}
	var result WorkInterrupt
	err := l.db.View(ctx, func(tx *store.Tx) error {
		g, err := authorize(tx, credential, Read)
		if err != nil {
			return err
		}
		if err = tx.Get(kindWorkInterrupt, workID, &result); err != nil {
			return err
		}
		_, _, _, err = bindingFor(tx, g, result.Work.Binding.ID, false)
		return err
	})
	if err != nil {
		return WorkInterrupt{}, err
	}
	return result, nil
}
