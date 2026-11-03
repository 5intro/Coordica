package runtime

import (
	"context"
	"errors"
	"math"
	"time"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func (p *BindingProducer) workBinding(tx *store.Tx, credential Credential, id string) (Grant, Binding, Run, error) {
	grant, err := authorize(tx, credential, StartWork)
	if err != nil {
		return grant, Binding{}, Run{}, err
	}
	b, task, run, err := bindingFor(tx, grant, id, true)
	if err == nil {
		err = liveBinding(grant, task, run)
	}
	if err == nil && (b.OwnerID != p.owner || b.Config != p.config) {
		err = ErrConflict
	}
	return grant, b, run, err
}
func (p *BindingProducer) PrepareWork(ctx context.Context, credential Credential, request PrepareWorkRequest) (Work, error) {
	if p == nil || !identifier(request.BindingID, 128) || !identifier(request.StreamID, 128) || request.Generation <= 0 || request.ExpectedVersion <= 0 || request.Text == "" || !content(request.Text) {
		return Work{}, ErrInvalid
	}
	if _, ok := p.port.(WorkPort); !ok {
		return Work{}, ErrInvalid
	}
	id, err := freshID("work")
	if err != nil {
		return Work{}, err
	}
	command, err := freshID("command")
	if err != nil {
		return Work{}, err
	}
	var binding Binding
	var run Run
	var stream EventStream
	return mutate(p.ledger, ctx, credential, StartWork, request.Key, "work.prepared", request,
		func(tx *store.Tx, grant Grant) (err error) {
			_, binding, run, err = p.workBinding(tx, credential, request.BindingID)
			if err == nil {
				stream, _, err = streamFor(tx, grant, request.StreamID)
			}
			if err == nil && (stream.BindingID != binding.ID || stream.State != "open" || binding.Generation != request.Generation) {
				err = ErrConflict
			}
			return err
		}, func(tx *store.Tx, _ Grant) (Work, string, bool, error) {
			if binding.State != "ready" || binding.CleanupStopped {
				return Work{}, "", false, ErrWorkBusy
			}
			if binding.Version != request.ExpectedVersion || binding.Version == math.MaxInt64 || run.Version == math.MaxInt64 {
				return Work{}, "", false, ErrConflict
			}
			var slot workSlot
			err := tx.Get(kindWorkSlot, binding.ID, &slot)
			if err != nil && !errors.Is(err, domain.ErrNotFound) {
				return Work{}, "", false, err
			}
			if slot.WorkID != "" || slot.Generation == math.MaxInt64 {
				return Work{}, "", false, ErrWorkBusy
			}
			slot.Generation++
			slot.WorkID = id
			w := Work{ID: id, CommandID: command, Generation: slot.Generation, Binding: binding, StreamID: stream.ID, Source: stream.Source, InputID: id,
				Version: 1, State: "prepared", CreatedAt: time.Now().UTC()}
			if err = putNew(tx, kindWork, id, w); err == nil {
				err = putNew(tx, kindWorkInput, id, workInput{WorkID: id, Text: request.Text})
			}
			if err == nil {
				err = tx.Put(kindWorkSlot, binding.ID, slot)
			}
			binding.State, binding.Outcome = "busy", "work_prepared"
			binding.Version++
			run.NativeDisposition, run.Version = "work_pending", run.Version+1
			if err == nil {
				err = tx.Put(kindBinding, binding.ID, binding)
			}
			if err == nil {
				err = tx.Put(kindRun, run.ID, run)
			}
			return w, id, true, err
		})
}

func workMatches(w Work, b Binding) bool {
	return workIdentityMatches(w, b) && !b.CleanupStopped
}
func workIdentityMatches(w Work, b Binding) bool {
	return w.Binding.ID == b.ID && w.Binding.Generation == b.Generation && w.Binding.OwnerID == b.OwnerID && w.Binding.Config == b.Config &&
		w.Binding.NativeID == b.NativeID && w.Binding.ContextID == b.ContextID && w.Binding.ContextVersion == b.ContextVersion && w.Binding.CommandID == b.CommandID &&
		w.Binding.ApplicationID == b.ApplicationID && w.Binding.TaskID == b.TaskID && w.Binding.RunID == b.RunID && w.Binding.RunGeneration == b.RunGeneration &&
		w.Binding.PrincipalID == b.PrincipalID && w.Binding.AuthorizationVersion == b.AuthorizationVersion
}

// Disclosure remains fenced after this command's own unknown/stop writes; it
// does not mistake those facts for a different owner, source or logical Work.
func (p *BindingProducer) workDisclosure(tx *store.Tx, credential Credential, original Work) error {
	g, b, _, err := p.workBinding(tx, credential, original.Binding.ID)
	if err != nil {
		return err
	}
	if !workIdentityMatches(original, b) {
		return ErrConflict
	}
	var w Work
	var slot workSlot
	if err = tx.Get(kindWork, original.ID, &w); err != nil {
		return err
	}
	if err = tx.Get(kindWorkSlot, b.ID, &slot); err != nil {
		return err
	}
	if w.CommandID != original.CommandID || w.Generation != original.Generation || slot.WorkID != w.ID || slot.Generation != w.Generation {
		return ErrConflict
	}
	stream, _, err := streamFor(tx, g, w.StreamID)
	if err != nil {
		return err
	}
	if stream.Source != w.Source {
		return ErrConflict
	}
	return nil
}
func (p *BindingProducer) workCurrent(tx *store.Tx, credential Credential, id string) (Grant, Binding, Run, Work, error) {
	var w Work
	if err := tx.Get(kindWork, id, &w); err != nil {
		return Grant{}, Binding{}, Run{}, w, err
	}
	g, b, r, err := p.workBinding(tx, credential, w.Binding.ID)
	if err == nil && !workMatches(w, b) {
		err = ErrConflict
	}
	return g, b, r, w, err
}

// workExecution fences the one logical command as well as its physical source.
func (p *BindingProducer) workExecution(tx *store.Tx, credential Credential, original Work) (Grant, Binding, Run, Work, workSlot, error) {
	g, b, r, w, err := p.workCurrent(tx, credential, original.ID)
	var slot workSlot
	if err == nil {
		err = tx.Get(kindWorkSlot, b.ID, &slot)
	}
	if err == nil && (w.CommandID != original.CommandID || w.Generation != original.Generation || w.State != "claimed" || slot.WorkID != w.ID || slot.Generation != w.Generation || b.State != "busy") {
		err = ErrConflict
	}
	var stream EventStream
	if err == nil {
		err = tx.Get(kindEventStream, w.StreamID, &stream)
	}
	if err == nil && (stream.State != "open" || stream.Source != w.Source || stream.BindingID != b.ID || stream.Generation != b.Generation || stream.OwnerID != b.OwnerID) {
		err = ErrConflict
	}
	return g, b, r, w, slot, err
}

func (l *Ledger) ReadWork(ctx context.Context, credential Credential, id string) (Work, error) {
	if l == nil || l.db == nil || !identifier(id, 128) {
		return Work{}, ErrInvalid
	}
	var w Work
	err := l.db.View(ctx, func(tx *store.Tx) error {
		g, err := authorize(tx, credential, Read)
		if err != nil {
			return err
		}
		if err = tx.Get(kindWork, id, &w); err != nil {
			return err
		}
		if w.Binding.ApplicationID != g.ApplicationID {
			return ErrForbidden
		}
		_, _, _, err = bindingFor(tx, g, w.Binding.ID, false)
		return err
	})
	if err != nil {
		return Work{}, err
	}
	return w, nil
}
