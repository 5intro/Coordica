package app

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestChatBlocksPaginationIncludesEveryGroupExactlyOnce(t *testing.T) {
	f := newServiceFixture(t)
	a, _ := chatFixture(t, f)
	ctx := context.Background()
	want := map[string]bool{"decision_low": true, "plan_low": true}
	if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		for i := 0; i < 25; i++ {
			id := fmt.Sprintf("task_hold_%02d", i)
			want[id] = true
			if e := tx.Put("task", id, domain.Task{ID: id, Version: 1, HoldReason: "paused"}); e != nil {
				return e
			}
		}
		if e := tx.Put("decision", "decision_low", domain.Decision{ID: "decision_low", Version: 1, Status: "open", Kind: "human_decision"}); e != nil {
			return e
		}
		return tx.Put("planning_run", "plan_low", domain.PlanningRun{ID: "plan_low", Version: 1, State: "blocked"})
	}); e != nil {
		t.Fatal(e)
	}
	seen := map[string]bool{}
	cursor := ""
	for pages := 0; ; pages++ {
		if pages > 3 {
			t.Fatal("pagination did not terminate")
		}
		page, e := f.s.ChatList(ctx, a, domain.ChatListQuery{Kind: "blocks", Limit: 20, Cursor: cursor})
		if e != nil {
			t.Fatal(e)
		}
		for _, item := range page.Items {
			if seen[item.ID] || item.ID <= cursor {
				t.Fatal("duplicate/nonadvancing item", item.ID)
			}
			seen[item.ID] = true
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if !reflect.DeepEqual(want, seen) {
		t.Fatal("skipped aggregation group", want, seen)
	}
}

// Include SQLite's idempotency and outbox tables, which the public Snapshot
// deliberately excludes, in the rejection invariant.
func chatMutationCounts(t *testing.T, f *serviceFixture) []int {
	t.Helper()
	db, e := sql.Open("sqlite", filepath.Join(f.s.Dir, "coordica.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	out := []int{}
	for _, table := range []string{"records", "audit", "outbox", "idempotency"} {
		var n int
		if e = db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); e != nil {
			t.Fatal(e)
		}
		out = append(out, n)
	}
	return out
}

func TestNodeChatIssuanceAndRevokeRequirePersistedCredentialBeforeReplay(t *testing.T) {
	for _, scenario := range []string{"missing", "revoked", "mismatched_binding", "forged_node"} {
		t.Run(scenario, func(t *testing.T) {
			f := newServiceFixture(t)
			ctx := context.Background()
			_, grant := nodeChatFixture(t, f, f.node, "original")
			_, second := nodeChatFixture(t, f, f.node, "second")
			if _, e := f.s.RevokeChatCredential(ctx, f.node, "revoke_original", grant.Credential.ID); e != nil {
				t.Fatal(e)
			}
			actor := f.node
			if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
				all, e := store.List[domain.Credential](tx, "credential")
				if e != nil {
					return e
				}
				for _, c := range all {
					if c.ID != f.node.ID || c.Role != "node" {
						continue
					}
					switch scenario {
					case "missing":
						c.ID = "unrelated_credential"
					case "revoked":
						c.Revoked = true
					case "mismatched_binding":
						c.NodeID = "different_node"
					case "forged_node":
						actor.NodeID = "different_node"
					}
					return tx.Put("credential", c.Hash, c)
				}
				return fmt.Errorf("missing fixture credential")
			}); e != nil {
				t.Fatal(e)
			}
			before := f.snapshot(t)
			counts := chatMutationCounts(t, f)
			for _, key := range []string{"second", "fresh"} {
				if _, e := f.s.CreateNodeChatCredential(ctx, actor, key, domain.ChatCredentialRequest{Name: "Shared node host"}); e == nil {
					t.Fatal("mint/replay trusted Actor", key)
				}
			}
			for _, input := range []struct{ key, id string }{{"revoke_original", grant.Credential.ID}, {"fresh_revoke", second.Credential.ID}} {
				if _, e := f.s.RevokeChatCredential(ctx, actor, input.key, input.id); e == nil {
					t.Fatal("revoke/replay trusted Actor", input)
				}
			}
			if !reflect.DeepEqual(before, f.snapshot(t)) || !reflect.DeepEqual(counts, chatMutationCounts(t, f)) {
				t.Fatal("rejected authority mutated durable state")
			}
		})
	}
}

func TestNodeChatGrantDerivesPersistedBindingWhenActorOmitsNode(t *testing.T) {
	f := newServiceFixture(t)
	actor := f.node
	actor.NodeID = ""
	_, g := nodeChatFixture(t, f, actor, "derived")
	if g.Credential.NodeID != f.node.NodeID {
		t.Fatal("binding not derived", g)
	}
}

func TestAutomationAllowsLongFinitePlanningAndConsultationRuns(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	settings := f.snapshot(t).Automation
	for i, pair := range [][2]int64{{3601, 14401}, {domain.MaxRunDurationSeconds, domain.MaxTaskBudgetSeconds}} {
		settings.PlanningTimeoutSeconds, settings.PlanningBudgetSeconds = pair[0], pair[1]
		raw, e := f.s.ConfigureAutomation(ctx, f.owner, fmt.Sprintf("long_%d", i), domain.AutomationRequest{ExpectedVersion: settings.Version, Settings: settings})
		settings = decodeTest[domain.AutomationSettings](t, raw, e)
	}
	for i, pair := range [][2]int64{{domain.MaxRunDurationSeconds + 1, domain.MaxTaskBudgetSeconds}, {3601, domain.MaxTaskBudgetSeconds + 1}, {3601, 3600}} {
		bad := settings
		bad.PlanningTimeoutSeconds, bad.PlanningBudgetSeconds = pair[0], pair[1]
		before := f.snapshot(t)
		if _, e := f.s.ConfigureAutomation(ctx, f.owner, fmt.Sprintf("bad_%d", i), domain.AutomationRequest{ExpectedVersion: settings.Version, Settings: bad}); e == nil {
			t.Fatal("unbounded settings accepted", pair)
		}
		if !reflect.DeepEqual(before, f.snapshot(t)) {
			t.Fatal("rejected settings changed state")
		}
	}
	a, _ := chatFixture(t, f)
	consultTest(t, f, a, 0, "long_consult", "consultation")
	r, _, _, e := (&PlanningWorker{Service: f.s}).reserve(ctx)
	if e != nil || r.GrantSeconds != domain.MaxRunDurationSeconds {
		t.Fatal("consultation retained short cap", r, e)
	}
}
