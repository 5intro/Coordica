package app

import (
	"context"
	"encoding/json"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func (c Coordination) List(ctx context.Context, a Actor, q domain.WorkListQuery) (domain.WorkContextPage, error) {
	out := domain.WorkContextPage{Items: []domain.WorkContextSummary{}}
	if err := domain.ValidateWorkListQuery(q); err != nil {
		return out, err
	}
	err := c.Service.DB.View(ctx, func(tx *store.Tx) error {
		r, err := newWorkReader(tx, a)
		if err != nil {
			return err
		}
		cursor, err := r.cursor(q.Cursor, "topics", domain.WorkAnchor{}, q.Kind)
		if err != nil {
			return err
		}
		notes, err := store.ReadWorkNotes(tx, store.WorkRead{ProjectID: r.project.ID, AuthorID: r.authorFilter(),
			Upper: cursor.Upper, Before: cursor.Before, Limit: q.Limit, Kind: q.Kind}, true)
		if err != nil {
			return err
		}
		more := len(notes) > q.Limit
		if more {
			notes = notes[:q.Limit]
		}
		for index, note := range notes {
			current, e := r.anchor(note.Anchor, true)
			if e != nil {
				return e
			}
			note = visibleWorkNote(note, a)
			note.Historical = !current.Available || note.AnchorStamp != current.Reference.Stamp
			out.Items = append(out.Items, domain.WorkContextSummary{Anchor: note.Anchor, Current: current, Latest: note})
			data, e := json.Marshal(out)
			if e != nil {
				return e
			}
			if len(data) > domain.MaxWorkContextBytes-4096 {
				out.Items = out.Items[:len(out.Items)-1]
				more = true
				if index == 0 {
					return domain.Fail("context_limit", "saved work is too large to display")
				}
				break
			}
		}
		if more && len(out.Items) > 0 {
			cursor.Before = out.Items[len(out.Items)-1].Latest.Sequence
			out.NextCursor = encodeWorkCursor(cursor)
		}
		return nil
	})
	if err == nil {
		out.QueriedAt = c.Service.Now()
	}
	return out, err
}

func readWorkReferences(r workReader, q domain.WorkContextQuery, out *domain.WorkContext) error {
	cursor, err := r.cursor(q.ReferenceCursor, "references", q.Anchor, "")
	if err != nil {
		return err
	}
	refs, err := store.ReadWorkReferences(r.tx, store.WorkRead{ProjectID: r.project.ID, AuthorID: r.authorFilter(), Anchor: q.Anchor,
		Upper: cursor.Upper, AfterReference: cursor.Reference, Limit: domain.MaxWorkReferences})
	if err != nil {
		return err
	}
	if len(refs) > domain.MaxWorkReferences {
		refs = refs[:domain.MaxWorkReferences]
		last := refs[len(refs)-1]
		cursor.Reference = last.Kind + ":" + last.ID
		out.NextReferenceCursor = encodeWorkCursor(cursor)
	}
	refs = append(refs, q.References...)
	natural, err := naturalWorkReferences(r, q.Anchor)
	if err != nil {
		return err
	}
	refs = append(refs, natural...)
	seen := map[string]int{}
	for _, ref := range refs {
		key := ref.Kind + ":" + ref.ID
		if index, found := seen[key]; found {
			if ref.Stamp != "" && out.References[index].Reference != ref {
				out.References[index].Stale = true
			}
			continue
		}
		f, e := r.fact(ref)
		if e == domain.ErrNotFound {
			f = unavailableWorkFact(ref)
		} else if e != nil {
			return e
		}
		if ref.Stamp != "" && f.Reference != ref {
			f.Stale = true
		}
		seen[key] = len(out.References)
		out.References = append(out.References, f)
	}
	return nil
}

func naturalWorkReferences(r workReader, a domain.WorkAnchor) ([]domain.WorkReference, error) {
	refs := []domain.WorkReference{}
	if a.Kind == "project" && !r.nodeBound() {
		for _, kind := range []string{"initialization", "goal"} {
			ref := domain.WorkReference{Kind: kind, ID: r.project.ID}
			if _, err := r.fact(ref); err == nil {
				refs = append(refs, ref)
			} else if err != domain.ErrNotFound {
				return nil, err
			}
		}
	}
	if a.Kind == "task" {
		var t domain.Task
		if err := r.tx.Get("task", a.ID, &t); err != nil {
			return nil, err
		}
		if !r.canReadTask(t) {
			return refs, nil
		}
		if t.CurrentRunID != "" {
			refs = append(refs, domain.WorkReference{Kind: "run", ID: t.CurrentRunID})
		}
		if !r.nodeBound() && t.CurrentCandidateID != "" {
			refs = append(refs, domain.WorkReference{Kind: "candidate", ID: t.CurrentCandidateID})
		}
	}
	return refs, nil
}
