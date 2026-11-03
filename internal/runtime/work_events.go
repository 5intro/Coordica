package runtime

import (
	"errors"
	"math"

	"coordica/internal/domain"
	"coordica/internal/store"
)

type workObservation struct {
	First, Last int64
	State       string
	Conflict    bool
}

func workObservationKey(streamID, nativeTurn string) string { return compound(streamID, nativeTurn) }
func workNativeKey(w Work, nativeTurn string) string {
	return compound(w.Binding.Config.Target.NodeID, w.Binding.Config.ProfileRef, w.Binding.NativeID.Harness, w.Binding.NativeID.Value, nativeTurn)
}

func observeWorkEvent(tx *store.Tx, stream EventStream, input EventInput, sequence int64) error {
	if input.Kind != "execution.ended" && input.Kind != "execution.failed" || input.Fact.NativeTurnID == "" {
		return nil
	}
	state := input.Fact.State
	if state != "completed" && state != "failed" && state != "interrupted" {
		return nil
	}
	key := workObservationKey(stream.ID, input.Fact.NativeTurnID)
	var observation workObservation
	err := tx.Get(kindWorkObservation, key, &observation)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	if observation.First == 0 {
		observation.First = input.SourceSequence
		observation.State = state
	}
	if observation.State != state {
		observation.Conflict = true
	}
	if input.SourceSequence > observation.Last {
		observation.Last = input.SourceSequence
	}
	if err = tx.Put(kindWorkObservation, key, observation); err != nil {
		return err
	}
	// This is a low-frequency terminal index, not a scan of event JSON. No
	// unknown native ID is guessed to belong to the current starting Work.
	var slot workSlot
	if err = tx.Get(kindWorkSlot, stream.BindingID, &slot); errors.Is(err, domain.ErrNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	if slot.WorkID == "" {
		return nil
	}
	var w Work
	if err = tx.Get(kindWork, slot.WorkID, &w); err != nil {
		return err
	}
	if w.State != "native_accepted" || w.StreamID != stream.ID || w.Source != stream.Source || w.Binding.Generation != stream.Generation ||
		w.Binding.OwnerID != stream.OwnerID || w.Observed.NativeTurnID != input.Fact.NativeTurnID || input.SourceSequence <= w.Observed.AfterSourceSequence {
		return nil
	}
	var b Binding
	var r Run
	if err = tx.Get(kindBinding, w.Binding.ID, &b); err != nil {
		return err
	}
	if err = tx.Get(kindRun, w.Binding.RunID, &r); err != nil {
		return err
	}
	if !workMatches(w, b) || b.State != "busy" || stream.State != "open" || slot.Generation != w.Generation {
		return nil
	}
	if err = applyWorkObservation(tx, &w, &b, &r, &slot, observation); err != nil {
		return err
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
		err = audit(tx, w.Binding.PrincipalID, "work.terminal_observed", w.ID)
	}
	return err
}

func applyWorkObservation(tx *store.Tx, w *Work, b *Binding, r *Run, slot *workSlot, observation workObservation) error {
	if w.Version == math.MaxInt64 || b.Version == math.MaxInt64 || r.Version == math.MaxInt64 {
		return ErrConflict
	}
	if observation.First <= w.Observed.AfterSourceSequence || observation.Conflict {
		w.State = "delivery_unknown"
		b.State, b.Outcome = "unknown", "work_terminal_conflict"
		r.NativeDisposition = "work_unknown"
	} else {
		w.State = "native_ended"
		w.Observed.Outcome, w.Observed.State = "ended", observation.State
		w.Observed.Evidence = "source_terminal"
		slot.WorkID = ""
		b.State, b.Outcome = "ready", "work_ended"
		r.NativeDisposition = "session_bound"
	}
	w.Version++
	b.Version++
	r.Version++
	return nil
}
