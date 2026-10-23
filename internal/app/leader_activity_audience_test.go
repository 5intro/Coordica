package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestLeaderActivityProseIsOwnerOnlyAcrossReadsAndFollow(t *testing.T) {
	f := newServiceFixture(t)
	chat, _ := chatFixture(t, f)
	ref := activityRun(t, f)
	secret := "私密问答松石钟楼检查参数天鹅绒茶叶"
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		init := domain.InitializationOperation{ID: "private_init", State: "running", Generation: 1, InitializationVersion: 1}
		if err := tx.Put("initialization_operation", init.ID, init); err != nil {
			return err
		}
		return store.AppendLeaderActivity(tx, []domain.LeaderActivityEvent{
			{Execution: ref, Key: secret + "key", Kind: "message", Text: secret},
			{Execution: domain.LeaderExecutionRef{Kind: "initialization", ID: init.ID, Generation: 1, InputVersion: 1},
				Key: secret + "init", Kind: "message", Text: secret},
			{Execution: ref, Key: secret + "tool", Kind: "tool_started", Text: secret},
			{Execution: ref, Key: secret + "unknown", Kind: secret, Text: secret},
		})
	}); err != nil {
		t.Fatal(err)
	}
	activities := LeaderActivities{Service: f.s}
	owner, err := activities.Read(context.Background(), f.owner, domain.LeaderActivityQuery{Limit: 10})
	if err != nil || len(owner.Events) != 4 || owner.Events[0].Text != secret {
		t.Fatal("Owner lost private display evidence", owner, err)
	}
	assertSafe := func(page domain.LeaderActivityPage) {
		t.Helper()
		data, _ := json.Marshal(page)
		if strings.Contains(string(data), secret) || len(page.Events) != 4 || page.Cursor != owner.Cursor {
			t.Fatal("limited activity contains original prose or lost cursor identity", string(data))
		}
	}
	page, err := activities.Read(context.Background(), chat, domain.LeaderActivityQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	assertSafe(page)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = activities.Follow(ctx, chat, domain.LeaderActivityQuery{Limit: 10}, func(page domain.LeaderActivityPage) error {
		assertSafe(page)
		cancel()
		return ctx.Err()
	})
}
