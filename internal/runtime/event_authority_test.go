package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"

	"coordica/internal/store"
)

type sourceAuthorityPort struct {
	countedBindingPort
	released int
}

func (p *sourceAuthorityPort) Captured(_ context.Context, _ NativeCreate, id NativeSessionID, _ int) (SourceStamp, []EventInput, *SourceEnd, error) {
	return SourceStamp{Epoch: "authority-source", Evidence: "native_sequence"}, []EventInput{eventInput(Binding{NativeID: id}, "pending", 1)}, nil, nil
}
func (p *sourceAuthorityPort) ReleaseCaptured(context.Context, NativeCreate, NativeSessionID, int64) error {
	p.released++
	return nil
}

func TestUnauthorizedSourceDrainCannotStopActiveResource(t *testing.T) {
	for _, scenario := range []string{"other-principal", "updated-authorization"} {
		t.Run(scenario, func(t *testing.T) {
			port := &sourceAuthorityPort{}
			f, p, task, run := bindingFixture(t, port)
			b := prepareFixture(t, f, p, task, run)
			b, err := p.Produce(t.Context(), f.cred, b.ID)
			if err != nil {
				t.Fatal(err)
			}
			s, err := p.OpenEventStream(t.Context(), f.cred, b.ID, SourceStamp{Epoch: "authority-source", Evidence: "native_sequence"})
			if err != nil {
				t.Fatal(err)
			}
			credential := f.cred
			if scenario == "other-principal" {
				credential = otherSourceCredential(t, f)
			} else if _, err = f.manager.ConfigureGrant(t.Context(), f.config, 2); err != nil {
				t.Fatal(err)
			}
			beforeB, beforeR, beforeS := sourceState(t, f, b, s)
			counts := eventCounts(t, f)
			_, err = p.DrainEvents(t.Context(), credential, b.ID, 8)
			afterB, afterR, afterS := sourceState(t, f, b, s)
			if !errors.Is(err, ErrForbidden) || port.stops.Load() != 0 || port.released != 0 || beforeB != afterB || beforeR != afterR || beforeS != afterS {
				t.Errorf("unauthorized fact drain stopped/changed active resource: err=%v stops=%d capture releases=%d", err, port.stops.Load(), port.released)
			}
			sameEventCounts(t, counts, eventCounts(t, f))
		})
	}
}

func sourceState(t *testing.T, f ledgerFixture, binding Binding, stream EventStream) (Binding, Run, EventStream) {
	t.Helper()
	var b Binding
	var r Run
	var s EventStream
	if err := f.db.View(t.Context(), func(tx *store.Tx) error {
		if err := tx.Get(kindBinding, binding.ID, &b); err != nil {
			return err
		}
		if err := tx.Get(kindRun, binding.RunID, &r); err != nil {
			return err
		}
		return tx.Get(kindEventStream, stream.ID, &s)
	}); err != nil {
		t.Fatal(err)
	}
	return b, r, s
}
func otherSourceCredential(t *testing.T, f ledgerFixture) Credential {
	t.Helper()
	g := f.config
	g.ID = "other-principal"
	g.Token = strings.Repeat("other-source-fixture-", 3)
	if _, err := f.manager.ConfigureGrant(t.Context(), g, 0); err != nil {
		t.Fatal(err)
	}
	return Credential{PrincipalID: g.ID, Token: g.Token}
}

func TestNewActiveSourceTerminalRequiresOriginalRunAuthority(t *testing.T) {
	for _, scenario := range []string{"other-principal", "updated-authorization"} {
		t.Run(scenario, func(t *testing.T) {
			f, p, b, s := eventFixture(t)
			input := eventInput(b, "first", 1)
			if _, err := p.AcceptEvent(t.Context(), f.cred, s.ID, input); err != nil {
				t.Fatal(err)
			}
			credential := f.cred
			if scenario == "other-principal" {
				credential = otherSourceCredential(t, f)
			} else if _, err := f.manager.ConfigureGrant(t.Context(), f.config, 2); err != nil {
				t.Fatal(err)
			}
			if receipt, err := p.AcceptEvent(t.Context(), credential, s.ID, input); err != nil || !receipt.Duplicate {
				t.Fatalf("historical receipt was not shared: %+v %v", receipt, err)
			}
			beforeB, beforeR, beforeS := sourceState(t, f, b, s)
			beforeCounts := eventCounts(t, f)
			err := p.ReportSourceEnd(t.Context(), credential, s.ID, SourceEnd{Reason: "transport_lost", Gap: true, LastObserved: 1})
			afterB, afterR, afterS := sourceState(t, f, b, s)
			if !errors.Is(err, ErrForbidden) || beforeB != afterB || beforeR != afterR || beforeS != afterS {
				t.Errorf("historical source authority changed active resource: err=%v Binding=%s→%s RunVersion=%d→%d Stream=%s→%s", err, beforeB.State, afterB.State, beforeR.Version, afterR.Version, beforeS.State, afterS.State)
			}
			sameEventCounts(t, beforeCounts, eventCounts(t, f))
		})
	}
}

func TestStoppedSourceDiagnosticSharingDoesNotRewriteResourceFacts(t *testing.T) {
	f, p, b, s := eventFixture(t)
	if _, err := p.AcceptEvent(t.Context(), f.cred, s.ID, eventInput(b, "first", 1)); err != nil {
		t.Fatal(err)
	}
	_, proof, err := p.cleanup(t.Context(), NativeCreate{BindingID: b.ID, OwnerID: b.OwnerID, Generation: b.Generation, Config: b.Config}, b.NativeID)
	if err != nil {
		t.Fatal(err)
	}
	if err = p.ReportSourceEnd(t.Context(), f.cred, s.ID, SourceEnd{Reason: "transport_lost", Gap: true, LastObserved: 1, Stop: proof}); err != nil {
		t.Fatal(err)
	}
	credential := otherSourceCredential(t, f)
	beforeB, beforeR, _ := sourceState(t, f, b, s)
	if err = p.ReportSourceEnd(t.Context(), credential, s.ID, SourceEnd{Reason: "transport_lost", Gap: true, LastObserved: 2}); err != nil {
		t.Fatal(err)
	}
	afterB, afterR, afterS := sourceState(t, f, b, s)
	if beforeB != afterB || beforeR != afterR || afterS.ObservedThrough != 2 || !afterS.Stop.TreeStopped {
		t.Fatalf("stopped diagnostic mutated resource or lost facts: BindingVersion=%d→%d RunVersion=%d→%d Stream=%+v", beforeB.Version, afterB.Version, beforeR.Version, afterR.Version, afterS)
	}
}
