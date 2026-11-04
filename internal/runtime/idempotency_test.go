package runtime

import (
	"errors"
	"reflect"
	"testing"

	"coordica/internal/domain"
)

func TestCurrentAuthorizationPrecedesReceiptReplay(t *testing.T) {
	f := fixtureLedger(t)
	request := SubmitTaskRequest{Key: "submit", Title: "Private task", Context: "private context material"}
	if _, err := f.ledger.SubmitTask(t.Context(), f.cred, request); err != nil {
		t.Fatal(err)
	}
	for _, credential := range []Credential{{PrincipalID: f.cred.PrincipalID, Token: "wrong-credential-which-is-long-enough"},
		{PrincipalID: "unregistered-owner", Token: f.cred.Token}} {
		if _, err := f.ledger.SubmitTask(t.Context(), credential, request); !errors.Is(err, ErrForbidden) {
			t.Fatalf("caller-selected identity obtained cached response: %v", err)
		}
	}
	f.config.Enabled = false
	if _, err := f.manager.ConfigureGrant(t.Context(), f.config, 1); err != nil {
		t.Fatal(err)
	}
	reopenFixture(t, &f)
	if _, err := f.ledger.SubmitTask(t.Context(), f.cred, request); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked caller received cached response: %v", err)
	}
}

func TestRequestAndSourceDeduplicationPreserveVersionsAndFanout(t *testing.T) {
	f := fixtureLedger(t)
	first := submitFixture(t, f, "first-task")
	second := submitFixture(t, f, "second-task")
	request := ContextRequest{Key: "context", TaskID: first.Task.ID, ExpectedVersion: 1, ExpectedContextVersion: 1,
		Source: Source{ID: "source", EventID: "event-1"}, Content: "new complete context"}
	input, err := f.ledger.UpdateContext(t.Context(), f.cred, request)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"context", "retry-with-different-key"} {
		request.Key = key
		again, err := f.ledger.UpdateContext(t.Context(), f.cred, request)
		if err != nil || !reflect.DeepEqual(input, again) {
			t.Fatalf("successful v1->v2 request could not replay: %+v %v", again, err)
		}
	}
	current := readFixture(t, f, first.Task.ID)
	if current.Task.Version != 2 || current.Task.ContextVersion != 2 || current.Task.RunGeneration != 0 || current.Task.CurrentRunID != "" {
		t.Fatalf("duplicate/context-only input dispatched work or advanced versions: %+v", current.Task)
	}
	request.Key, request.Content = "source-conflict", "changed event body"
	if _, err := f.ledger.UpdateContext(t.Context(), f.cred, request); !errors.Is(err, ErrConflict) {
		t.Fatalf("same event different content was overwritten: %v", err)
	}
	request.Key = "context"
	var conflict *domain.Error
	if _, err := f.ledger.UpdateContext(t.Context(), f.cred, request); !errors.As(err, &conflict) || conflict.Code != "idempotency_conflict" {
		t.Fatalf("same request key different content was accepted: %v", err)
	}
	request.Key, request.TaskID, request.Content = "fanout", second.Task.ID, input.Content
	fanout, err := f.ledger.UpdateContext(t.Context(), f.cred, request)
	if err != nil || fanout.ID == input.ID || fanout.TaskID != second.Task.ID {
		t.Fatalf("source event swallowed explicit second task fanout: %+v %v", fanout, err)
	}
	read, err := f.ledger.ReadInput(t.Context(), f.cred, input.ID)
	if err != nil || !reflect.DeepEqual(read, input) {
		t.Fatalf("immutable input could not be recovered: %+v %v", read, err)
	}
}
