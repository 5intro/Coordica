package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"coordica/internal/domain"
	"coordica/internal/store"
)

type eventDataset struct {
	Epoch string `json:"epoch"`
	Count int64  `json:"count"`
}

func (p *BindingProducer) eventTarget(tx *store.Tx, credential Credential, id string, active bool) (Grant, Binding, Run, error) {
	grant, err := authorize(tx, credential, SessionControl)
	if err != nil {
		return grant, Binding{}, Run{}, err
	}
	binding, task, run, err := bindingFor(tx, grant, id, active)
	if err == nil && active {
		err = liveBinding(grant, task, run)
	}
	if err == nil && (binding.Config != p.config || binding.OwnerID != p.owner || active && binding.State != "ready" && binding.State != "busy" && binding.State != "unknown") {
		err = ErrConflict
	}
	return grant, binding, run, err
}

func streamFor(tx *store.Tx, grant Grant, id string) (EventStream, Binding, error) {
	var stream EventStream
	if err := tx.Get(kindEventStream, id, &stream); err != nil {
		return stream, Binding{}, err
	}
	if stream.ApplicationID != grant.ApplicationID {
		return EventStream{}, Binding{}, ErrForbidden
	}
	binding, _, _, err := bindingFor(tx, grant, stream.BindingID, false)
	if err == nil && (binding.Generation != stream.Generation || binding.OwnerID != stream.OwnerID) {
		err = ErrCursorExpired
	}
	var dataset eventDataset
	if err == nil {
		err = tx.Get(kindEventDataset, "current", &dataset)
	}
	if err == nil && dataset.Epoch != stream.DatasetEpoch {
		err = ErrCursorExpired
	}
	return stream, binding, err
}

func (p *BindingProducer) OpenEventStream(ctx context.Context, credential Credential, bindingID string, source SourceStamp) (EventStream, error) {
	if p == nil || !identifier(bindingID, 128) || !identifier(source.Epoch, 128) || source.Evidence != "native_sequence" && source.Evidence != "local_observed" {
		return EventStream{}, ErrInvalid
	}
	var result EventStream
	err := p.ledger.db.Update(ctx, func(tx *store.Tx) error {
		grant, binding, _, err := p.eventTarget(tx, credential, bindingID, false)
		if err != nil {
			return err
		}
		slot := compound(binding.ID, binding.OwnerID, digest(binding.Generation))
		var previous string
		if err = tx.Get(kindEventStreamSlot, slot, &previous); err == nil {
			var b Binding
			result, b, err = streamFor(tx, grant, previous)
			_ = b
			if err == nil && result.Source != source {
				err = ErrConflict
			}
			return err
		} else if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if _, binding, _, err = p.eventTarget(tx, credential, bindingID, true); err != nil {
			return err
		}
		if binding.State != "ready" {
			return ErrConflict
		}
		var dataset eventDataset
		if err = tx.Get(kindEventDataset, "current", &dataset); errors.Is(err, domain.ErrNotFound) {
			dataset.Epoch, err = freshID("dataset")
			if err == nil {
				err = putNew(tx, kindEventDataset, "current", dataset)
			}
		}
		if err != nil {
			return err
		}
		id, err := freshID("stream")
		if err != nil {
			return err
		}
		result = EventStream{ID: id, DatasetEpoch: dataset.Epoch, Source: source, ApplicationID: grant.ApplicationID, TaskID: binding.TaskID,
			RunID: binding.RunID, BindingID: binding.ID, OwnerID: binding.OwnerID, Generation: binding.Generation, NativeID: binding.NativeID, State: "open"}
		if err = putNew(tx, kindEventStream, id, result); err == nil {
			err = putNew(tx, kindEventStreamSlot, slot, id)
		}
		if err == nil {
			err = audit(tx, grant.ID, "events.opened", id)
		}
		return err
	})
	if err != nil {
		return EventStream{}, err
	}
	return result, nil
}

func validEvent(input EventInput) bool {
	if !identifier(input.EventID, 256) || input.SourceSequence <= 0 || input.SourceSequence == math.MaxInt64 || input.OccurredAt.IsZero() ||
		input.Fact.Bytes < 0 || input.Fact.Bytes > 256<<10 || input.Fact.Tokens < 0 || input.Fact.Tokens > 1<<50 {
		return false
	}
	for _, id := range []string{input.Fact.NativeTurnID, input.Fact.NativeRequestID} {
		if id != "" && !identifier(id, 512) {
			return false
		}
	}
	switch input.Kind {
	case "execution.started", "execution.ended", "execution.failed", "execution.waiting", "output.delta", "tool.activity",
		"request.created", "request.resolved", "session.updated", "usage.observed":
	default:
		return false
	}
	switch input.Fact.State {
	case "", "started", "completed", "failed", "interrupted", "waiting", "created", "resolved", "observed":
		return true
	}
	return false
}

func markSourceUnknown(tx *store.Tx, binding Binding, run Run, stopped bool) error {
	if binding.Version == math.MaxInt64 || run.Version == math.MaxInt64 {
		return ErrConflict
	}
	binding.State, binding.Outcome, binding.CleanupStopped = "unknown", "unknown", stopped
	binding.Version++
	run.NativeDisposition, run.Version = "session_unknown", run.Version+1
	if err := tx.Put(kindBinding, binding.ID, binding); err != nil {
		return err
	}
	return tx.Put(kindRun, run.ID, run)
}

func priorEvent(tx *store.Tx, stream EventStream, input EventInput, d string) (EventReceipt, bool, error) {
	byID, err := tx.RuntimeEventByID(stream.ApplicationID, stream.ID, input.EventID)
	if err == nil {
		if byID.Digest != d || byID.SourceSequence != input.SourceSequence {
			return EventReceipt{}, false, ErrConflict
		}
		return EventReceipt{Sequence: byID.Sequence, SourceSequence: byID.SourceSequence, Duplicate: true}, true, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return EventReceipt{}, false, err
	}
	if _, err = tx.RuntimeEventBySource(stream.ApplicationID, stream.ID, input.SourceSequence); err == nil {
		return EventReceipt{}, false, ErrConflict
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return EventReceipt{}, false, err
	}
	return EventReceipt{}, false, nil
}

func (p *BindingProducer) AcceptEvent(ctx context.Context, credential Credential, streamID string, input EventInput) (EventReceipt, error) {
	if p == nil || !identifier(streamID, 128) || !validEvent(input) {
		return EventReceipt{}, ErrInvalid
	}
	var receipt EventReceipt
	var capacity bool
	err := p.ledger.db.Update(ctx, func(tx *store.Tx) error {
		grant, err := authorize(tx, credential, SessionControl)
		if err != nil {
			return err
		}
		stream, binding, err := streamFor(tx, grant, streamID)
		if err != nil {
			return err
		}
		_, current, run, err := p.eventTarget(tx, credential, binding.ID, false)
		if err != nil {
			return err
		}
		if current.Generation != stream.Generation || input.NativeID != stream.NativeID {
			return ErrConflict
		}
		d := digest(struct {
			Stream string
			Input  EventInput
		}{streamID, input})
		var found bool
		receipt, found, err = priorEvent(tx, stream, input, d)
		if err != nil || found {
			return err
		}
		_, current, run, err = p.eventTarget(tx, credential, binding.ID, true)
		if err != nil {
			return err
		}
		if stream.State != "open" {
			return ErrEventBackpressure
		}
		var dataset eventDataset
		if err = tx.Get(kindEventDataset, "current", &dataset); err != nil {
			return err
		}
		if stream.Count >= maxStreamEvents || dataset.Count >= maxDatasetEvents {
			stream.Gap, stream.State, stream.EndReason = true, "failed", "capacity"
			capacity = true
			if err = tx.Put(kindEventStream, stream.ID, stream); err == nil {
				err = markSourceUnknown(tx, current, run, false)
			}
			if err == nil {
				err = audit(tx, grant.ID, "events.capacity_exhausted", stream.ID)
			}
			return err
		}
		event := RuntimeEvent{SchemaVersion: 1, StreamID: streamID, ApplicationID: stream.ApplicationID, TaskID: stream.TaskID,
			RunID: stream.RunID, BindingID: stream.BindingID, Generation: stream.Generation, Input: input, ReceivedAt: time.Now().UTC()}
		raw, err := json.Marshal(event)
		if err != nil {
			return err
		}
		sequence, err := tx.AppendRuntimeEvent(stream.ApplicationID, stream.ID, store.RuntimeEventRow{EventID: input.EventID,
			Sequence: stream.LastSequence + 1, SourceSequence: input.SourceSequence, Digest: d, Data: raw})
		if err != nil {
			return err
		}
		if err = observeWorkEvent(tx, stream, input, sequence); err != nil {
			return err
		}
		if input.SourceSequence > stream.LastSourceSequence+1 {
			stream.Gap = true
		}
		if input.SourceSequence > stream.LastSourceSequence {
			stream.LastSourceSequence = input.SourceSequence
		}
		if input.SourceSequence > stream.ObservedThrough {
			stream.ObservedThrough = input.SourceSequence
		}
		stream.LastSequence, stream.Count = sequence, stream.Count+1
		dataset.Count++
		for stream.ContiguousSourceSequence < stream.LastSourceSequence {
			_, err = tx.RuntimeEventBySource(stream.ApplicationID, stream.ID, stream.ContiguousSourceSequence+1)
			if errors.Is(err, domain.ErrNotFound) {
				break
			}
			if err != nil {
				return err
			}
			stream.ContiguousSourceSequence++
		}
		if err = tx.Put(kindEventStream, stream.ID, stream); err == nil {
			err = tx.Put(kindEventDataset, "current", dataset)
		}
		if err == nil {
			err = audit(tx, grant.ID, "events.accepted", stream.ID)
		}
		receipt = EventReceipt{Sequence: sequence, SourceSequence: input.SourceSequence}
		return err
	})
	if err != nil {
		return EventReceipt{}, err
	}
	if capacity {
		return EventReceipt{}, ErrEventBackpressure
	}
	return receipt, nil
}

func (p *BindingProducer) ReportSourceEnd(ctx context.Context, credential Credential, streamID string, end SourceEnd) error {
	if p == nil || !identifier(streamID, 128) || end.LastObserved < 0 {
		return ErrInvalid
	}
	switch end.Reason {
	case "closed", "transport_lost", "backpressure", "unsupported":
	default:
		return ErrInvalid
	}
	return p.ledger.db.Update(ctx, func(tx *store.Tx) error {
		grant, err := authorize(tx, credential, SessionControl)
		if err != nil {
			return err
		}
		stream, binding, err := streamFor(tx, grant, streamID)
		if err != nil {
			return err
		}
		_, binding, run, err := p.eventTarget(tx, credential, binding.ID, false)
		if err != nil {
			return err
		}
		if end.LastObserved < stream.LastSourceSequence {
			return ErrConflict
		}
		call := NativeCreate{BindingID: binding.ID, OwnerID: binding.OwnerID, Generation: binding.Generation, Config: binding.Config}
		stopped := scopedStop(end.Stop, call, binding.NativeID)
		if end.Stop.TreeStopped && !stopped {
			return ErrConflict
		}
		if (stream.State == "closed" || stream.State == "failed" && stream.EndReason != "capacity") &&
			end.LastObserved <= stream.ObservedThrough && (!stopped || stream.Stop.TreeStopped) &&
			(!end.Gap || stream.Gap) && (end.Reason == "closed" || stream.State == "failed") {
			return nil
		}
		// Historical receipts/known-stopped diagnostics are shared app facts.
		// A new terminal on a possibly active resource can close ingestion and
		// trigger cleanup, so it still requires the original live Run authority.
		resourceActive := !binding.CleanupStopped && !stream.Stop.TreeStopped && binding.State != "released"
		if resourceActive {
			task, err := taskFor(tx, grant, binding.TaskID)
			if err != nil {
				return err
			}
			if err = liveBinding(grant, task, run); err != nil {
				return err
			}
			if _, err = targetFor(tx, grant, run.Target, true); err != nil {
				return err
			}
		}
		failed, previousReason := stream.State == "failed", stream.EndReason
		if end.LastObserved > stream.ObservedThrough {
			stream.ObservedThrough = end.LastObserved
		}
		if stopped {
			stream.Stop = end.Stop
		}
		stream.Gap = stream.Gap || end.Gap || end.LastObserved > stream.LastSourceSequence
		stream.State, stream.EndReason = "closed", end.Reason
		if failed || stream.Gap || end.Reason != "closed" || binding.State == "ready" {
			stream.Gap, stream.State = true, "failed"
			if failed {
				stream.EndReason = previousReason
			}
			task, err := taskFor(tx, grant, binding.TaskID)
			if err != nil {
				return err
			}
			if resourceActive && task.CurrentRunID == run.ID && run.State == "registered" && binding.State != "released" && binding.State != "releasing" {
				if err = markSourceUnknown(tx, binding, run, binding.CleanupStopped || stopped); err != nil {
					return err
				}
			}
		}
		if err = tx.Put(kindEventStream, stream.ID, stream); err != nil {
			return err
		}
		return audit(tx, grant.ID, "events.source_ended", stream.ID)
	})
}
