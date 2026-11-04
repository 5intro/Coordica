package runtime

import (
	"context"
	"encoding/json"

	"coordica/internal/store"
)

func eventCursorFor(tx *store.Tx, credential Credential, id string) (Grant, EventCursor, EventStream, error) {
	grant, err := authorize(tx, credential, SessionControl)
	if err != nil {
		return grant, EventCursor{}, EventStream{}, err
	}
	var cursor EventCursor
	if err = tx.Get(kindEventCursor, id, &cursor); err != nil {
		return grant, cursor, EventStream{}, err
	}
	if cursor.ApplicationID != grant.ApplicationID || cursor.PrincipalID != grant.ID {
		return grant, EventCursor{}, EventStream{}, ErrForbidden
	}
	stream, _, err := streamFor(tx, grant, cursor.StreamID)
	if err == nil && (stream.DatasetEpoch != cursor.DatasetEpoch || stream.Source.Epoch != cursor.SourceEpoch) {
		err = ErrCursorExpired
	}
	return grant, cursor, stream, err
}

func (l *Ledger) OpenEventCursor(ctx context.Context, credential Credential, streamID string) (EventCursor, error) {
	if l == nil || l.db == nil || !identifier(streamID, 128) {
		return EventCursor{}, ErrInvalid
	}
	id, err := freshID("cursor")
	if err != nil {
		return EventCursor{}, err
	}
	var cursor EventCursor
	err = l.db.Update(ctx, func(tx *store.Tx) error {
		grant, err := authorize(tx, credential, SessionControl)
		if err != nil {
			return err
		}
		stream, _, err := streamFor(tx, grant, streamID)
		if err != nil {
			return err
		}
		cursor = EventCursor{ID: id, ApplicationID: grant.ApplicationID, PrincipalID: grant.ID, StreamID: streamID,
			DatasetEpoch: stream.DatasetEpoch, SourceEpoch: stream.Source.Epoch}
		if err = putNew(tx, kindEventCursor, id, cursor); err != nil {
			return err
		}
		return audit(tx, grant.ID, "events.cursor_opened", id)
	})
	if err != nil {
		return EventCursor{}, err
	}
	return cursor, nil
}

// ReadEvents offers a bounded page from the last acknowledged position. Losing
// the response repeats the page; offered is not proof of client/native receipt.
func (l *Ledger) ReadEvents(ctx context.Context, credential Credential, id string, limit int) (EventPage, error) {
	if l == nil || l.db == nil || !identifier(id, 128) || limit < 1 || limit > 64 {
		return EventPage{}, ErrInvalid
	}
	var page EventPage
	err := l.db.Update(ctx, func(tx *store.Tx) error {
		grant, cursor, stream, err := eventCursorFor(tx, credential, id)
		if err != nil {
			return err
		}
		rows, err := tx.RuntimeEventPage(grant.ApplicationID, stream.ID, cursor.Acked, limit+1)
		if err != nil {
			return err
		}
		page = EventPage{Cursor: cursor, Stream: stream, Events: make([]RuntimeEvent, 0, limit), More: len(rows) > limit}
		if len(rows) > limit {
			rows = rows[:limit]
		}
		for _, row := range rows {
			var e RuntimeEvent
			if err = json.Unmarshal(row.Data, &e); err != nil {
				return err
			}
			if e.SchemaVersion != 1 || e.StreamID != stream.ID || e.ApplicationID != grant.ApplicationID || e.TaskID != stream.TaskID ||
				e.RunID != stream.RunID || e.BindingID != stream.BindingID || e.Generation != stream.Generation ||
				e.Input.EventID != row.EventID || e.Input.SourceSequence != row.SourceSequence || !validEvent(e.Input) ||
				row.Digest != digest(struct {
					Stream string
					Input  EventInput
				}{stream.ID, e.Input}) {
				return ErrConflict
			}
			e.Sequence = row.Sequence
			page.Events = append(page.Events, e)
		}
		if len(rows) > 0 && rows[len(rows)-1].Sequence > cursor.Offered {
			cursor.Offered = rows[len(rows)-1].Sequence
			page.Cursor = cursor
			if err = tx.Put(kindEventCursor, id, cursor); err != nil {
				return err
			}
			return audit(tx, grant.ID, "events.page_offered", id)
		}
		return nil
	})
	if err != nil {
		return EventPage{}, err
	}
	return page, nil
}

// AckEvents acknowledges only locally offered positions for this principal and
// stream. It does not acknowledge a native request or consume Task work.
func (l *Ledger) AckEvents(ctx context.Context, credential Credential, id string, through int64) (EventCursor, error) {
	if l == nil || l.db == nil || !identifier(id, 128) || through < 0 {
		return EventCursor{}, ErrInvalid
	}
	var result EventCursor
	err := l.db.Update(ctx, func(tx *store.Tx) error {
		grant, cursor, stream, err := eventCursorFor(tx, credential, id)
		if err != nil {
			return err
		}
		if through < cursor.Acked || through > cursor.Offered {
			return ErrConflict
		}
		result = cursor
		if through == cursor.Acked {
			return nil
		}
		rows, err := tx.RuntimeEventPage(grant.ApplicationID, stream.ID, through-1, 1)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].Sequence != through {
			return ErrConflict
		}
		result.Acked = through
		if err = tx.Put(kindEventCursor, id, result); err != nil {
			return err
		}
		return audit(tx, grant.ID, "events.cursor_acked", id)
	})
	if err != nil {
		return EventCursor{}, err
	}
	return result, nil
}
