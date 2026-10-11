package app

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"coordica/internal/domain"
)

func TestRunScopeAndBlockRejectRevokedAuthenticatedActorBeforeReplay(t *testing.T) {
	for _, operation := range []string{"scope", "block", "reason-only scope"} {
		for _, replay := range []bool{false, true} {
			name := "fresh"
			if replay {
				name = "replay"
			}
			t.Run(operation+"/"+name, func(t *testing.T) {
				ctx := context.Background()
				// The fixture authenticates this node before the owner revokes it.
				f := newServiceFixture(t)
				oldActor := f.node
				a := f.claim(t, "run")
				if a == nil {
					t.Fatal("missing assignment")
				}
				request := func() (json.RawMessage, error) {
					req := domain.RunScopeRequest{Generation: a.Run.Generation, Reason: "Need shared source"}
					switch operation {
					case "scope":
						req.Paths = []string{"src/shared/**"}
					case "block":
						return f.s.Block(ctx, oldActor, "request", a.Run.ID, req.Generation, req.Reason)
					}
					return f.s.RequestScope(ctx, oldActor, "request", a.Run.ID, req)
				}
				if replay {
					raw, err := request()
					if err != nil {
						t.Fatal(err)
					}
					repeated, err := request()
					if err != nil || string(repeated) != string(raw) {
						t.Fatal("live-node replay changed", err)
					}
				}
				if _, err := f.s.RevokeNode(ctx, f.owner, "revoke", oldActor.NodeID); err != nil {
					t.Fatal(err)
				}
				before := f.snapshot(t)
				raw, err := request()
				assertNodeConsoleError(t, err, "forbidden")
				if len(raw) != 0 {
					t.Fatal("revoked node received a mutation result")
				}
				if !reflect.DeepEqual(before, f.snapshot(t)) {
					t.Fatal("rejected request changed task, run, scope, budget or audit state")
				}
			})
		}
	}
}
