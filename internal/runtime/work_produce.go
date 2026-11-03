package runtime

import (
	"context"
	"errors"
	"math"
	"time"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func (p *BindingProducer) ProduceWork(ctx context.Context, credential Credential, id string) (Work, error) {
	if p == nil || !identifier(id, 128) {
		return Work{}, ErrInvalid
	}
	port, ok := p.port.(WorkPort)
	if !ok {
		return Work{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return Work{}, err
	}
	var w Work
	var text string
	err := p.ledger.db.Update(ctx, func(tx *store.Tx) error {
		g, b, _, current, err := p.workCurrent(tx, credential, id)
		if err != nil {
			return err
		}
		w = current
		var slot workSlot
		if err = tx.Get(kindWorkSlot, b.ID, &slot); err != nil {
			return err
		}
		if slot.WorkID != w.ID || b.State != "busy" || w.State != "prepared" || w.Version == math.MaxInt64 {
			return ErrOutcomeUnknown
		}
		var input workInput
		if err = tx.Get(kindWorkInput, w.InputID, &input); err != nil {
			return err
		}
		if input.WorkID != w.ID {
			return ErrConflict
		}
		text = input.Text
		w.State, w.Version, w.ClaimedAt = "claimed", w.Version+1, time.Now().UTC()
		if err = tx.Put(kindWork, w.ID, w); err != nil {
			return err
		}
		return audit(tx, g.ID, "work.claimed", w.ID)
	})
	if err != nil {
		return Work{}, err
	}
	call := WorkCall{Target: NativeCreate{BindingID: w.Binding.ID, OwnerID: w.Binding.OwnerID, Generation: w.Binding.Generation, Config: w.Binding.Config},
		NativeID: w.Binding.NativeID, WorkID: w.ID, CommandID: w.CommandID, WorkGeneration: w.Generation, Text: text, Effort: w.Binding.Config.Target.Effort}
	invokeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	observed, nativeErr := port.StartWork(invokeCtx, call)
	cancel()
	known := observed.SourceEpoch == w.Source.Epoch && observed.AfterSourceSequence >= 0 &&
		(nativeErr == nil && (observed.Outcome == "accepted" && observed.Evidence == "rpc_accepted" || observed.Outcome == "ended" && observed.Evidence == "rpc_terminal") ||
			nativeErr != nil && observed.NativeTurnID == "" && observed.State == "" && observed.Evidence == "" && (observed.Outcome == "rejected" || observed.Outcome == "not_sent"))
	if observed.Outcome == "accepted" && observed.State != "" && observed.State != "running" && observed.State != "waiting" {
		known = false
	}
	if observed.Outcome == "ended" && observed.State != "completed" && observed.State != "failed" && observed.State != "interrupted" {
		known = false
	}
	if observed.NativeTurnID != "" && !identifier(observed.NativeTurnID, 512) {
		known = false
		observed.NativeTurnID = ""
	}
	if observed.State != "" && observed.State != "running" && observed.State != "waiting" && observed.State != "completed" && observed.State != "failed" && observed.State != "interrupted" {
		known = false
		observed.State = ""
	}
	finishCtx, done := context.WithTimeout(context.WithoutCancel(ctx), rpcCleanupTimeout)
	if known {
		result, err := p.finishWork(finishCtx, credential, w, observed)
		if err == nil {
			done()
			if result.State == "delivery_unknown" {
				return result, ErrOutcomeUnknown
			}
			return result, nativeErr
		}
		nativeErr = errors.Join(nativeErr, err)
	}
	done()
	var proof BindingStop
	var stopErr error
	cleanupCtx, cleanupDone := context.WithTimeout(context.WithoutCancel(ctx), rpcCleanupTimeout)
	cleanupFence := p.ledger.db.View(cleanupCtx, func(tx *store.Tx) error { _, _, _, _, _, err := p.workExecution(tx, credential, w); return err })
	cleanupDone()
	if cleanupFence == nil {
		_, proof, stopErr = p.cleanup(ctx, call.Target, call.NativeID)
	}
	unknownCtx, done := context.WithTimeout(context.WithoutCancel(ctx), rpcCleanupTimeout)
	defer done()
	quarantineErr := p.unknownWork(unknownCtx, w, observed, proof)
	fenceErr := p.ledger.db.View(unknownCtx, func(tx *store.Tx) error { return p.workDisclosure(tx, credential, w) })
	if fenceErr != nil {
		return Work{}, errors.Join(ErrOutcomeUnknown, fenceErr)
	}
	if cleanupFence != nil {
		return Work{}, errors.Join(ErrOutcomeUnknown, cleanupFence)
	}
	return Work{}, errors.Join(ErrOutcomeUnknown, nativeErr, stopErr, quarantineErr)
}

func (p *BindingProducer) finishWork(ctx context.Context, credential Credential, original Work, observed NativeWork) (Work, error) {
	var result Work
	err := p.ledger.db.Update(ctx, func(tx *store.Tx) error {
		g, b, r, w, slot, err := p.workExecution(tx, credential, original)
		if err != nil {
			return err
		}
		if w.State != "claimed" || w.CommandID != original.CommandID || slot.WorkID != w.ID || w.Version == math.MaxInt64 || b.Version == math.MaxInt64 || r.Version == math.MaxInt64 {
			return ErrConflict
		}
		w.Observed, w.Version = observed, w.Version+1
		if observed.NativeTurnID != "" && (observed.Outcome == "accepted" || observed.Outcome == "ended") {
			if err = putNew(tx, kindWorkNative, workNativeKey(w, observed.NativeTurnID), w.ID); err != nil {
				return err
			}
		}
		switch observed.Outcome {
		case "accepted":
			w.State = "native_accepted"
			b.Outcome = "work_active"
			r.NativeDisposition = "work_accepted"
		case "ended":
			w.State = "native_ended"
			slot.WorkID = ""
			b.State, b.Outcome = "ready", "work_ended"
			r.NativeDisposition = "session_bound"
		case "rejected", "not_sent":
			w.State = "rejected"
			slot.WorkID = ""
			b.State, b.Outcome = "ready", "work_rejected"
			r.NativeDisposition = "session_bound"
		default:
			return ErrConflict
		}
		b.Version++
		r.Version++
		if w.State == "native_accepted" && observed.NativeTurnID != "" {
			var observation workObservation
			err = tx.Get(kindWorkObservation, workObservationKey(w.StreamID, observed.NativeTurnID), &observation)
			if err == nil {
				if err = applyWorkObservation(tx, &w, &b, &r, &slot, observation); err != nil {
					return err
				}
			} else if !errors.Is(err, domain.ErrNotFound) {
				return err
			}
		}
		if err = tx.Put(kindWork, w.ID, w); err == nil {
			err = tx.Put(kindWorkSlot, b.ID, slot)
		}
		if err == nil {
			err = tx.Put(kindBinding, b.ID, b)
		}
		if err == nil {
			err = tx.Put(kindRun, r.ID, r)
		}
		if err == nil {
			err = audit(tx, g.ID, "work.native_observed", w.ID)
		}
		result = w
		return err
	})
	return result, err
}

func (p *BindingProducer) unknownWork(ctx context.Context, original Work, observed NativeWork, proof BindingStop) error {
	return p.ledger.db.Update(ctx, func(tx *store.Tx) error {
		var w Work
		if err := tx.Get(kindWork, original.ID, &w); err != nil {
			return err
		}
		if w.CommandID != original.CommandID || w.Binding.Generation != original.Binding.Generation || w.Binding.OwnerID != original.Binding.OwnerID || w.Version == math.MaxInt64 {
			return ErrConflict
		}
		if w.State != "claimed" && w.State != "prepared" {
			return nil
		}
		w.State, w.Observed, w.Version = "delivery_unknown", observed, w.Version+1
		if scopedStop(proof, NativeCreate{BindingID: w.Binding.ID, OwnerID: w.Binding.OwnerID, Generation: w.Binding.Generation, Config: w.Binding.Config}, w.Binding.NativeID) {
			w.Stop = proof
		}
		if err := tx.Put(kindWork, w.ID, w); err != nil {
			return err
		}
		var b Binding
		if err := tx.Get(kindBinding, w.Binding.ID, &b); err != nil {
			return err
		}
		var slot workSlot
		if err := tx.Get(kindWorkSlot, b.ID, &slot); err != nil {
			return err
		}
		var source EventStream
		if err := tx.Get(kindEventStream, w.StreamID, &source); err != nil {
			return err
		}
		if workMatches(w, b) && b.State == "busy" && slot.WorkID == w.ID && slot.Generation == w.Generation && source.State == "open" && source.Source == w.Source {
			var r Run
			if err := tx.Get(kindRun, b.RunID, &r); err != nil {
				return err
			}
			if err := markSourceUnknown(tx, b, r, scopedStop(proof, NativeCreate{BindingID: b.ID, OwnerID: b.OwnerID, Generation: b.Generation, Config: b.Config}, b.NativeID)); err != nil {
				return err
			}
		}
		return audit(tx, "runtime:host", "work.delivery_unknown", w.ID)
	})
}
