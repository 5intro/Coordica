package app

import (
	"context"
	"encoding/json"

	"coordica/internal/domain"
	"coordica/internal/store"
)

// Coordination stores explicit discussion, not another task/approval state.
type Coordination struct{ Service *Service }

func (c Coordination) Record(ctx context.Context, a Actor, key string, in domain.WorkNoteInput) (json.RawMessage, error) {
	if err := domain.ValidateWorkNote(in); err != nil {
		return nil, err
	}
	source, err := sourceInput(a, in.Source)
	if err != nil {
		return nil, err
	}
	in.Source = source
	check := func(tx *store.Tx) error {
		r, e := newWorkReader(tx, a)
		if e != nil {
			return e
		}
		_, e = workWriteContext(r, in, false)
		return e
	}
	return c.Service.mutateWithCheck(ctx, a, key, "coordination.record", in, check, func(tx *store.Tx) (any, error) {
		if replay, found, e := chatSourceReplay(tx, "coordination.record", source, in); e != nil || found {
			return replay, e
		}
		r, e := newWorkReader(tx, a)
		if e != nil {
			return nil, e
		}
		current, e := workWriteContext(r, in, true)
		if e != nil {
			return nil, e
		}
		note := domain.WorkNote{ID: ID("note_"), ProjectID: r.project.ID, AuthorID: a.ID, ChatScope: r.binding.ChatScope, NodeID: r.binding.NodeID,
			Anchor: in.Anchor, AnchorStamp: current.Reference.Stamp, Kind: in.Kind, OriginalText: in.OriginalText, AssistantSummary: in.AssistantSummary,
			References: append([]domain.WorkReference{}, in.References...), Corrects: in.Corrects, Source: source, CreatedAt: c.Service.Now(),
			OriginalTextSHA256: Hash(in.OriginalText)}
		if a.Role == "owner" {
			note.ChatScope = "owner"
		}
		note, e = store.AppendWorkNote(tx, note)
		if e != nil {
			return nil, e
		}
		response := domain.WorkNoteReceipt{Note: visibleWorkNote(note, a), Disposition: "recorded_only"}
		if e = c.Service.audit(tx, a, "coordination.note.recorded", note.ID, ""); e != nil {
			return nil, e
		}
		return response, rememberChatSource(tx, "coordination.record", source, in, response)
	})
}

func workWriteContext(r workReader, in domain.WorkNoteInput, pinned bool) (domain.WorkFact, error) {
	f, err := r.anchor(in.Anchor, false)
	if err != nil {
		return f, err
	}
	if pinned && in.ExpectedStamp != f.Reference.Stamp {
		return f, domain.Fail("version_conflict", "work context changed; refresh before recording")
	}
	for _, ref := range in.References {
		current, e := r.fact(ref)
		if e != nil {
			return f, e
		}
		if pinned && current.Reference != ref {
			return f, domain.Fail("version_conflict", "a referenced object changed; refresh its current version")
		}
	}
	if in.Corrects != "" {
		prior, e := store.WorkNote(r.tx, in.Corrects, r.project.ID, r.authorFilter())
		if e != nil {
			return f, e
		}
		if prior.Anchor != in.Anchor {
			return f, domain.ErrNotFound
		}
	}
	return f, nil
}

func (c Coordination) Context(ctx context.Context, a Actor, q domain.WorkContextQuery) (domain.WorkContext, error) {
	out := domain.WorkContext{Anchor: q.Anchor, Notes: []domain.WorkNote{}, References: []domain.WorkFact{}}
	if err := domain.ValidateWorkContextQuery(q); err != nil {
		return out, err
	}
	err := c.Service.DB.View(ctx, func(tx *store.Tx) error {
		r, err := newWorkReader(tx, a)
		if err != nil {
			return err
		}
		out.Current, err = r.anchor(q.Anchor, true)
		if err != nil {
			return err
		}
		cursor, err := r.cursor(q.Cursor, "notes", q.Anchor, "")
		if err != nil {
			return err
		}
		notes, err := store.ReadWorkNotes(tx, store.WorkRead{ProjectID: r.project.ID, AuthorID: r.authorFilter(), Anchor: q.Anchor,
			Upper: cursor.Upper, Before: cursor.Before, Limit: q.Limit}, false)
		if err != nil {
			return err
		}
		if err = readWorkReferences(r, q, &out); err != nil {
			return err
		}
		for index := range notes {
			notes[index] = visibleWorkNote(notes[index], a)
		}
		return finishWorkNotes(&out, notes, q.Limit, cursor)
	})
	if err == nil {
		out.QueriedAt = c.Service.Now()
	}
	return out, err
}

func visibleWorkNote(note domain.WorkNote, a Actor) domain.WorkNote {
	if a.Role == "owner" {
		return note
	}
	text, summary := redacted(note.OriginalText), redacted(note.AssistantSummary)
	note.Redacted = text != note.OriginalText || summary != note.AssistantSummary
	note.OriginalText, note.AssistantSummary = text, summary
	return note
}

func finishWorkNotes(out *domain.WorkContext, notes []domain.WorkNote, limit int, cursor workCursor) error {
	more := len(notes) > limit
	if more {
		notes = notes[:limit]
	}
	for index, note := range notes {
		note.Historical = !out.Current.Available || note.AnchorStamp != out.Current.Reference.Stamp
		out.Notes = append(out.Notes, note)
		data, err := json.Marshal(out)
		if err != nil {
			return err
		}
		// Reserve room for the query timestamp and opaque continuation cursor.
		if len(data) > domain.MaxWorkContextBytes-4096 {
			out.Notes = out.Notes[:len(out.Notes)-1]
			more = true
			if index == 0 {
				return domain.Fail("context_limit", "work context too large; request fewer references")
			}
			break
		}
	}
	if more && len(out.Notes) > 0 {
		cursor.Before = out.Notes[len(out.Notes)-1].Sequence
		out.NextCursor = encodeWorkCursor(cursor)
	}
	return nil
}
