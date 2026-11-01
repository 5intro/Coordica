package app

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestWorkListAmbiguityAndSourceRevisionRequireFreshSelection(t *testing.T) {
	f := newServiceFixture(t)
	a, _ := chatFixture(t, f)
	ctx := context.Background()
	for index := 0; index < 2; index++ {
		task := f.task
		task.ID = fmt.Sprintf("task_match_%d", index)
		task.Title = "Similar work"
		if e := f.s.DB.Update(ctx, func(tx *store.Tx) error { return tx.Put("task", task.ID, task) }); e != nil {
			t.Fatal(e)
		}
		anchor := domain.WorkAnchor{Kind: "task", ID: task.ID}
		in := workInputTest(t, f, a, anchor, fmt.Sprintf("m_%d", index))
		_ = recordWorkTest(t, f, a, fmt.Sprintf("note_%d", index), in)
	}
	page, e := (Coordination{f.s}).List(ctx, a, domain.WorkListQuery{Kind: "task", Limit: 1})
	if e != nil || len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatal(page, e)
	}
	second, e := (Coordination{f.s}).List(ctx, a, domain.WorkListQuery{Kind: "task", Limit: 1, Cursor: page.NextCursor})
	if e != nil || len(second.Items) != 1 || second.Items[0].Anchor == page.Items[0].Anchor {
		t.Fatal("ambiguous work was selected/lost", second, e)
	}
	anchor := page.Items[0].Anchor
	stale := workInputTest(t, f, a, anchor, "before-revision")
	if e = f.s.DB.Update(ctx, func(tx *store.Tx) error {
		var task domain.Task
		if e := tx.Get("task", anchor.ID, &task); e != nil {
			return e
		}
		task.Version++
		task.Description = "New requirement contract"
		return tx.Put("task", task.ID, task)
	}); e != nil {
		t.Fatal(e)
	}
	current := workContextTest(t, f, a, anchor)
	if current.Current.Reference.Version == page.Items[0].Current.Reference.Version || !current.Notes[0].Historical {
		t.Fatal("summary treated old state as current")
	}
	if _, e = (Coordination{f.s}).Record(ctx, a, "old-selection", stale); e == nil {
		t.Fatal("old selection changed current discussion context")
	}
}

func TestWorkContextReadsUpdatedReplyWithoutNewHistoryEntry(t *testing.T) {
	f := newServiceFixture(t)
	a, _ := chatFixture(t, f)
	ctx := context.Background()
	q := consultTest(t, f, a, 0, "saved-question", "consultation")
	anchor := domain.WorkAnchor{Kind: "task", ID: f.task.ID}
	observed := workContextTest(t, f, a, anchor, domain.WorkReference{Kind: "consultation", ID: q.ID})
	in := workInputTest(t, f, a, anchor, "reference")
	in.Kind = "reference"
	in.OriginalText = ""
	in.References = []domain.WorkReference{observed.References[0].Reference}
	_ = recordWorkTest(t, f, a, "reference", in)
	before := workContextTest(t, f, a, anchor)
	w := PlanningWorker{Service: f.s, Executor: planningExecFunc(func(context.Context, string, string) (PlanningOutcome, error) {
		return PlanningOutcome{TreeStopped: true, Output: `{"reply":"New reply after reconnect"}`}, nil
	})}
	if e := w.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	after := workContextTest(t, f, a, anchor)
	if len(after.Notes) != len(before.Notes) || len(after.References) != 1 || after.References[0].Text != "New reply after reconnect" || !after.References[0].Stale || after.References[0].Reference.Stamp == before.References[0].Reference.Stamp {
		t.Fatal("creation sequence hid updated source reply", after)
	}
	// The accepted note remains replayable despite the referenced reply changing.
	_ = recordWorkTest(t, f, a, "reference", in)
	in.Source.MessageID = "different-message"
	if _, e := (Coordination{f.s}).Record(ctx, a, "stale-reference", in); e == nil {
		t.Fatal("old reply reference accepted for new write")
	}
	if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		return tx.Put("planning_document", "current", domain.PlanningDocument{Version: 1, Objective: "New goal"})
	}); e != nil {
		t.Fatal(e)
	}
	after = workContextTest(t, f, a, anchor)
	if !after.References[0].Stale {
		t.Fatal("goal change revived old consultation")
	}
}

func TestWorkContextBoundedPaginationScopeAndRestore(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	a, _ := chatFixture(t, f)
	anchor := domain.WorkAnchor{Kind: "task", ID: f.task.ID}
	for i := 0; i < 5; i++ {
		in := workInputTest(t, f, a, anchor, string(rune('a'+i)))
		in.OriginalText = strings.Repeat("<", 15000)
		_ = recordWorkTest(t, f, a, in.Source.MessageID, in)
	}
	query := domain.WorkContextQuery{Anchor: anchor, Limit: 100}
	page, e := (Coordination{f.s}).Context(ctx, a, query)
	if e != nil {
		t.Fatal(e)
	}
	data, _ := json.Marshal(page)
	if len(data) > domain.MaxWorkContextBytes || page.NextCursor == "" || len(page.Notes) == 0 {
		t.Fatal("unbounded/silently truncated page", len(data))
	}
	seen := map[string]bool{}
	for {
		for _, n := range page.Notes {
			if seen[n.ID] {
				t.Fatal("duplicate pagination")
			}
			seen[n.ID] = true
		}
		if page.NextCursor == "" {
			break
		}
		query.Cursor = page.NextCursor
		page, e = (Coordination{f.s}).Context(ctx, a, query)
		if e != nil {
			t.Fatal(e)
		}
	}
	if len(seen) != 5 {
		t.Fatal("lost saved originals", len(seen))
	}
	query.Cursor = ""
	query.Limit = 1
	first, e := (Coordination{f.s}).Context(ctx, a, query)
	if e != nil {
		t.Fatal(e)
	}
	query.Cursor = first.NextCursor
	if _, e = (Coordination{f.s}).Context(ctx, f.owner, query); e == nil {
		t.Fatal("cursor crossed identity")
	}
	query.Anchor.Kind = "project"
	query.Anchor.ID = f.snapshot(t).Project.ID
	if _, e = (Coordination{f.s}).Context(ctx, a, query); e == nil {
		t.Fatal("cursor crossed anchor")
	}
	query.Anchor = anchor
	// Offline backup includes notes, but restore rotates the data-instance epoch.
	backup, e := f.s.Backup(ctx, f.owner)
	if e != nil {
		t.Fatal(e)
	}
	dest := filepath.Join(t.TempDir(), "restored")
	if e = RestoreBackup(ctx, backup, dest); e != nil {
		t.Fatal(e)
	}
	db, e := store.Open(filepath.Join(dest, "coordica.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	restored := Coordination{New(db, dest)}
	if _, e = restored.Context(ctx, a, query); e == nil {
		t.Fatal("old chat credential survived restore")
	}
	// Compare same Owner binding so epoch, rather than changed principal, rejects it.
	ownerPage, e := (Coordination{f.s}).Context(ctx, f.owner, domain.WorkContextQuery{Anchor: anchor, Limit: 1})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = restored.Context(ctx, f.owner, domain.WorkContextQuery{Anchor: anchor, Limit: 1, Cursor: ownerPage.NextCursor}); e == nil {
		t.Fatal("restore accepted old cursor")
	}
	got, e := restored.Context(ctx, f.owner, domain.WorkContextQuery{Anchor: anchor, Limit: 1})
	if e != nil || len(got.Notes) != 1 || !got.Notes[0].Historical {
		t.Fatal("restore lost notes/old provenance", got, e)
	}
}

func TestWorkInitializationQuestionsFollowAuthoritativeState(t *testing.T) {
	f := newServiceFixture(t)
	a, _ := chatFixture(t, f)
	anchor := domain.WorkAnchor{Kind: "project", ID: f.snapshot(t).Project.ID}
	for _, state := range []struct{ status, want string }{{"interview", "awaiting_answer"}, {"analyzing", "processing"}, {"initialized", "closed"}} {
		if e := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
			return tx.Put("initialization", "current", domain.Initialization{Version: 3, Status: state.status, Result: &domain.InitializationResult{Questions: []domain.InitializationQuestion{{ID: "question_1", Text: "Which interface?"}}}})
		}); e != nil {
			t.Fatal(e)
		}
		got := workContextTest(t, f, a, anchor)
		found := false
		for _, ref := range got.References {
			if ref.Reference.Kind == "initialization" {
				found = true
				if len(ref.Questions) != 1 || ref.Questions[0].State != state.want {
					t.Fatal(ref)
				}
			}
		}
		if !found {
			t.Fatal("missing existing question reference")
		}
	}
}
