package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"coordica/internal/store"
)

func (p *BindingProducer) readTarget(tx *store.Tx, credential Credential, id string) (Binding, error) {
	grant, err := authorize(tx, credential, SessionControl)
	if err != nil {
		return Binding{}, err
	}
	binding, task, run, err := bindingFor(tx, grant, id, true)
	if err == nil {
		err = liveBinding(grant, task, run)
	}
	if err == nil && (binding.Config != p.config || binding.OwnerID != p.owner || binding.State != "ready") {
		err = ErrConflict
	}
	return binding, err
}

// ReadNative returns private transient history from an already-owned ready
// connection. It does not open/resume resources or put history in receipts.
func (p *BindingProducer) ReadNative(ctx context.Context, credential Credential, id string, includeTurns bool) (json.RawMessage, error) {
	if p == nil || !identifier(id, 128) {
		return nil, ErrInvalid
	}
	port, ok := p.port.(SessionPort)
	if !ok {
		return nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	commandID, err := freshID("command")
	if err != nil {
		return nil, err
	}
	var binding Binding
	var command BindingCommand
	err = p.ledger.db.Update(ctx, func(tx *store.Tx) error {
		var err error
		binding, err = p.readTarget(tx, credential, id)
		if err != nil {
			return err
		}
		command = BindingCommand{ID: commandID, BindingID: id, Generation: binding.Generation,
			OwnerID: p.owner, Operation: "read", State: "inflight"}
		return putNew(tx, kindBindingCommand, command.ID, command)
	})
	if err != nil {
		return nil, err
	}
	call := nativeCall(binding, command)
	readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	result, readErr := port.Read(readCtx, call, binding.NativeID, includeTurns)
	cancel()
	known := result.Outcome == "confirmed" && readErr == nil && json.Valid(result.History) ||
		readErr != nil && (result.Outcome == "rejected" || result.Outcome == "not_sent")
	finishCtx, done := context.WithTimeout(context.WithoutCancel(ctx), rpcCleanupTimeout)
	fenceErr := p.ledger.db.Update(finishCtx, func(tx *store.Tx) error {
		current, err := p.readTarget(tx, credential, id)
		if err != nil {
			return err
		}
		if current != binding {
			return ErrConflict
		}
		if known {
			command.State = "completed"
			command.Observed = NativeCreation{Outcome: result.Outcome, ID: binding.NativeID}
			return tx.Put(kindBindingCommand, command.ID, command)
		}
		return nil
	})
	done()
	if known && fenceErr == nil {
		if readErr != nil {
			return nil, readErr
		}
		return append(json.RawMessage(nil), result.History...), nil
	}
	if fenceErr != nil {
		// An expired reader must receive neither private history nor a native
		// RPCError carrying private Message/Data through errors.As/Unwrap.
		readErr = fenceErr
	}
	stopped, stop, stopErr := p.cleanup(ctx, call, binding.NativeID)
	if fenceErr != nil && stopErr != nil {
		stopErr = ErrStopUnconfirmed
	}
	unknownCtx, done := context.WithTimeout(context.WithoutCancel(ctx), rpcCleanupTimeout)
	defer done()
	err = p.quarantine(unknownCtx, binding, command, NativeCreation{Outcome: "read_unknown", ID: binding.NativeID}, stopped, stop)
	return nil, errors.Join(ErrOutcomeUnknown, readErr, stopErr, err)
}
