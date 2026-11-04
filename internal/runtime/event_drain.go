package runtime

import (
	"context"
	"errors"

	"coordica/internal/store"
)

type DrainReceipt struct {
	StreamID        string
	Accepted        int
	AcceptedThrough int64 // Source observation position, not native consumption.
}

// DrainEvents explicitly copies one bounded capture batch into SQLite. The
// transport collector never waits on this transaction or an event subscriber.
func (p *BindingProducer) DrainEvents(ctx context.Context, credential Credential, bindingID string, limit int) (DrainReceipt, error) {
	if p == nil || !identifier(bindingID, 128) || limit < 1 || limit > 32 {
		return DrainReceipt{}, ErrInvalid
	}
	port, ok := p.port.(EventPort)
	if !ok {
		return DrainReceipt{}, ErrInvalid
	}
	var binding Binding
	err := p.ledger.db.View(ctx, func(tx *store.Tx) error {
		_, b, _, err := p.eventTarget(tx, credential, bindingID, false)
		binding = b
		return err
	})
	if err != nil {
		return DrainReceipt{}, err
	}
	call := NativeCreate{BindingID: binding.ID, OwnerID: binding.OwnerID, Generation: binding.Generation, Config: binding.Config}
	stamp, batch, end, err := port.Captured(ctx, call, binding.NativeID, limit)
	if err != nil {
		return DrainReceipt{}, err
	}
	if len(batch) > limit {
		return DrainReceipt{}, ErrInvalid
	}
	for i := 1; i < len(batch); i++ {
		if batch[i].SourceSequence <= batch[i-1].SourceSequence {
			return DrainReceipt{}, ErrInvalid
		}
	}
	stream, err := p.OpenEventStream(ctx, credential, bindingID, stamp)
	if err != nil {
		return DrainReceipt{}, err
	}
	receipt := DrainReceipt{StreamID: stream.ID}
	for _, input := range batch {
		if _, err = p.AcceptEvent(ctx, credential, stream.ID, input); err != nil {
			if errors.Is(err, ErrEventBackpressure) || errors.Is(err, ErrConflict) || errors.Is(err, ErrCursorExpired) || errors.Is(err, ErrForbidden) || errors.Is(err, ErrInvalid) {
				// A denied first write is not authority to stop an active source.
				// Reuse the execution fence before any cleanup I/O, including when
				// an existing stream made historical capture lookup permissible.
				fenceCtx, done := context.WithTimeout(context.WithoutCancel(ctx), rpcCleanupTimeout)
				fenceErr := p.disclosureFence(fenceCtx, credential, binding, BindingCommand{ID: binding.CommandID, OwnerID: binding.OwnerID, Generation: binding.Generation})
				done()
				if fenceErr != nil {
					return receipt, errors.Join(err, fenceErr)
				}
				stopped, proof, _ := p.cleanup(ctx, call, binding.NativeID)
				if !stopped {
					proof = BindingStop{}
				}
				endCtx, done := context.WithTimeout(context.WithoutCancel(ctx), rpcCleanupTimeout)
				observed := max(stream.ObservedThrough, stream.LastSourceSequence, receipt.AcceptedThrough, input.SourceSequence)
				endErr := p.reportSourceEnd(endCtx, credential, stream.ID, SourceEnd{Reason: "backpressure", Gap: true, LastObserved: observed, Stop: proof}, true)
				done()
				return receipt, errors.Join(err, endErr)
			}
			return receipt, err
		}
		receipt.Accepted++
		receipt.AcceptedThrough = input.SourceSequence
		if err = port.ReleaseCaptured(ctx, call, binding.NativeID, input.SourceSequence); err != nil {
			return receipt, err
		}
	}
	if end != nil {
		err = p.ReportSourceEnd(ctx, credential, stream.ID, *end)
	}
	return receipt, err
}
