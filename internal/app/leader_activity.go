package app

import (
	"context"
	"encoding/json"
	"time"

	"coordica/internal/domain"
	"coordica/internal/store"
)

// LeaderActivities owns display authorization, execution bindings and bounded
// projection. It cannot start a Leader, alter a decision or dispatch work.
type LeaderActivities struct{ Service *Service }

func (l LeaderActivities) Read(ctx context.Context, actor Actor, query domain.LeaderActivityQuery) (domain.LeaderActivityPage, error) {
	page := domain.LeaderActivityPage{}
	if err := domain.ValidateLeaderActivityQuery(query); err != nil {
		return page, err
	}
	err := l.Service.DB.View(ctx, func(tx *store.Tx) error {
		if err := conversationCheck(actor)(tx); err != nil {
			return err
		}
		heads, err := readableLeaderHeads(tx, actor, query)
		if err != nil {
			return err
		}
		refs := make([]domain.LeaderExecutionRef, 0, len(heads))
		for _, head := range heads {
			if query.Generation > head.Ref.Generation {
				return domain.Fail("invalid_input", "activity generation is ahead of the execution")
			}
			refs = append(refs, head.Ref)
		}
		page, err = store.ReadLeaderActivity(tx, query, refs)
		projectLeaderActivityAudience(actor, &page)
		page.Executions = heads
		page.QueriedAt = l.Service.Now()
		return err
	})
	return page, err
}

func readableLeaderHeads(tx *store.Tx, actor Actor, query domain.LeaderActivityQuery) ([]domain.LeaderExecution, error) {
	result := []domain.LeaderExecution{}
	clientID := ""
	if actor.Role != "owner" {
		clientID = actor.ID
	}
	for _, kind := range []string{"initialization", "planning"} {
		if query.Kind != "" && query.Kind != kind {
			continue
		}
		heads, err := store.LeaderExecutionHeads(tx, kind, query.ID, clientID)
		if err != nil {
			return nil, err
		}
		for _, head := range heads {
			if actor.Role != "owner" && head.Purpose == domain.ConsultationPurpose {
				var conversation domain.ConversationRequest
				if err := tx.Get("conversation", head.SourceID, &conversation); err != nil {
					return nil, err
				}
				if conversation.Source.ClientID != actor.ID {
					if query.ID != "" {
						return nil, domain.ErrNotFound
					}
					continue
				}
			}
			result = append(result, head)
		}
	}
	if query.ID != "" && len(result) == 0 {
		return nil, domain.ErrNotFound
	}
	return result, nil
}

// Subscribe before querying; committed changes wake readers without progress
// polling. Slow transports hold no database transaction and cannot block the
// bounded process-event producer. Every read rechecks persisted chat authority.
func (l LeaderActivities) Follow(ctx context.Context, actor Actor, query domain.LeaderActivityQuery,
	send func(domain.LeaderActivityPage) error,
) error {
	previous := ""
	for {
		changed := l.Service.DB.Changes()
		page, err := l.Read(ctx, actor, query)
		if err != nil {
			return err
		}
		projection := page
		projection.QueriedAt = time.Time{}
		data, _ := json.Marshal(projection)
		fingerprint := Hash(string(data))
		if fingerprint != previous {
			if err := send(page); err != nil {
				return err
			}
			previous = fingerprint
		}
		query.After = page.Cursor
		if page.HasMore {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}
