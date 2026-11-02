package appserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func activeFixtureTurn(t *testing.T, scenario string) (*Peer, Turn) {
	t.Helper()
	p, thread := openThreadFixture(t, scenario)
	turn, err := thread.StartText(t.Context(), "fixture input", "high")
	if err != nil {
		t.Fatal(err)
	}
	nextFrame(t, p)
	return p, turn
}

func fixtureRequests(t *testing.T, p *Peer, turn Turn) (Request, Request) {
	t.Helper()
	call(t, p, "fixture/requests", map[string]any{}, nil)
	question, err := turn.Request(nextFrame(t, p))
	if err != nil {
		t.Fatal(err)
	}
	approval, err := turn.Request(nextFrame(t, p))
	if err != nil {
		t.Fatal(err)
	}
	return question, approval
}

func TestTypedQuestionAndApprovalRetainNativeIdentityAndChoices(t *testing.T) {
	p, turn := activeFixtureTurn(t, "typed")
	question, approval := fixtureRequests(t, p, turn)
	if string(question.ID()) != "7" || string(approval.ID()) != `"7"` {
		t.Fatal("native numeric/string IDs conflated")
	}
	questions := question.Questions()
	if len(questions) != 1 || !questions[0].IsSecret || !questions[0].IsOther || questions[0].Options[0].Label != "fixture option" {
		t.Fatalf("native question metadata lost: %+v", questions)
	}
	if blocking, known := question.QuestionBlocking(); !blocking || !known {
		t.Fatal("blocking fact lost")
	}
	questions[0].ID = "changed"
	questions[0].Options[0].Label = "changed"
	if question.Questions()[0].ID != "q1" || question.Questions()[0].Options[0].Label != "fixture option" {
		t.Fatal("question copy mutated callback")
	}
	if _, err := question.Answer(map[string][]string{"unknown": {"fixture answer"}}); !errors.Is(err, ErrInvalidAnswer) {
		t.Fatal(err)
	}
	if receipt, err := question.Answer(map[string][]string{"q1": {"fixture answer"}}); err != nil || receipt.Disposition != "queued" {
		t.Fatalf("answer receipt: %+v %v", receipt, err)
	}
	if _, err := question.Answer(map[string][]string{"q1": {"another answer"}}); !errors.Is(err, ErrStaleRequest) {
		t.Fatal(err)
	}
	if approval.DecisionSource() != "native" {
		t.Fatal("native decisions relabeled")
	}
	choices := approval.Decisions()
	choices[0][1] = 'x'
	if string(approval.Decisions()[0]) != `"decline"` {
		t.Fatal("choice copy mutated callback")
	}
	if _, err := approval.Decide(json.RawMessage(`"accept"`)); !errors.Is(err, ErrInvalidAnswer) {
		t.Fatal("offered decisions broadened", err)
	}
	if _, err := approval.Decide(json.RawMessage(`"decline"`)); err != nil {
		t.Fatal(err)
	}
	var status struct {
		Replies    int
		LastResult struct {
			Decision string `json:"decision"`
		} `json:"lastResult"`
	}
	call(t, p, "fixture/status", map[string]any{}, &status)
	if status.Replies != 2 || status.LastResult.Decision != "decline" {
		t.Fatalf("incorrect native reply: %+v", status)
	}
}

func TestNativeResolvedInvalidatesBeforeConsumerDrainsAndDoesNotApprove(t *testing.T) {
	p, turn := activeFixtureTurn(t, "typed")
	question, approval := fixtureRequests(t, p, turn)
	call(t, p, "fixture/resolve", map[string]any{}, nil)
	// The notification remains queued; the RPC response proves receive already
	// processed it. Approval ID "7" must not be confused with question ID 7.
	if _, err := question.Answer(map[string][]string{"q1": {"late"}}); !errors.Is(err, ErrStaleRequest) {
		t.Fatalf("resolved callback accepted: %v", err)
	}
	if _, err := approval.Decide(json.RawMessage(`"decline"`)); err != nil {
		t.Fatal(err)
	}
	frame := nextFrame(t, p)
	if frame.Method != "serverRequest/resolved" {
		t.Fatal(frame)
	}
	var status struct{ Replies int }
	call(t, p, "fixture/status", map[string]any{}, &status)
	if status.Replies != 1 {
		t.Fatalf("resolved became automatic approval: %+v", status)
	}
}

func TestExplicitExecutionInvalidationRejectsPendingAndLateCallbacks(t *testing.T) {
	p, turn := activeFixtureTurn(t, "typed")
	question, approval := fixtureRequests(t, p, turn)
	if err := turn.InvalidateRequests(); err != nil {
		t.Fatal(err)
	}
	if err := turn.InvalidateRequests(); err != nil {
		t.Fatal("invalidation was not idempotent", err)
	}
	if _, err := question.Answer(map[string][]string{"q1": {"late"}}); !errors.Is(err, ErrStaleRequest) {
		t.Fatal(err)
	}
	if _, err := approval.Decide(json.RawMessage(`"decline"`)); !errors.Is(err, ErrStaleRequest) {
		t.Fatal(err)
	}
	call(t, p, "fixture/requests", map[string]any{}, nil)
	for range 2 {
		if _, err := turn.Request(nextFrame(t, p)); !errors.Is(err, ErrStaleRequest) {
			t.Fatalf("late request revived closed target: %v", err)
		}
	}
	var status struct {
		Replies int
		Turns   int
	}
	call(t, p, "fixture/status", map[string]any{}, &status)
	if status.Replies != 0 || status.Turns != 1 {
		t.Fatalf("invalidation sent a native decision or new work: %+v", status)
	}
}

func TestTurnCompletionAloneDoesNotInventBackgroundStopOrRequestResolution(t *testing.T) {
	p, turn := activeFixtureTurn(t, "typed")
	question, _ := fixtureRequests(t, p, turn)
	call(t, p, "fixture/finish", map[string]any{}, nil)
	if nextFrame(t, p).Method != "turn/completed" {
		t.Fatal("missing native fact")
	}
	if _, err := question.Answer(map[string][]string{"q1": {"explicit answer"}}); err != nil {
		t.Fatalf("completion invented request resolution: %v", err)
	}
}

func TestInterruptRPCDoesNotInventTerminalEvent(t *testing.T) {
	p, turn := activeFixtureTurn(t, "typed-interrupt-pending")
	if err := turn.Interrupt(t.Context()); err != nil {
		t.Fatal(err)
	}
	var status struct {
		Active bool
		Turns  int
	}
	call(t, p, "fixture/status", map[string]any{}, &status)
	if !status.Active || status.Turns != 1 {
		t.Fatalf("RPC turned into a stop claim: %+v", status)
	}
	select {
	case frame := <-p.Events():
		t.Fatalf("invented terminal event: %+v", frame)
	default:
	}
}

func TestTypedRequestRejectsWrongConnectionMutationAndDuplicateQuestions(t *testing.T) {
	p, turn := activeFixtureTurn(t, "typed")
	other, otherTurn := activeFixtureTurn(t, "typed")
	call(t, p, "fixture/requests", map[string]any{}, nil)
	frame := nextFrame(t, p)
	if _, err := otherTurn.Request(frame); !errors.Is(err, ErrStaleRequest) {
		t.Fatal("cross-connection callback accepted", err)
	}
	changed := frame
	changed.Params = append(json.RawMessage{}, frame.Params...)
	changed.Params = json.RawMessage(strings.Replace(string(changed.Params), "Choose a fixture value", "Changed question", 1))
	if _, err := turn.Request(changed); !errors.Is(err, ErrStaleRequest) {
		t.Fatal("mutated callback accepted", err)
	}
	nextFrame(t, p)
	call(t, other, "fixture/malformed-question", map[string]any{}, nil)
	if _, err := otherTurn.Request(nextFrame(t, other)); !errors.Is(err, ErrProtocol) {
		t.Fatal("ambiguous questions accepted", err)
	}
	nextFrame(t, other)
}

func TestRequestInvalidationTombstonesStayBounded(t *testing.T) {
	p := &Peer{native: map[string]nativeRequest{}, invalidScopes: map[requestScope]bool{}}
	thread := Thread{peer: p, id: "thread"}
	for i := range maxNativeIDs {
		if err := (Turn{thread: thread, id: fmt.Sprint(i)}).InvalidateRequests(); err != nil {
			t.Fatal(err)
		}
	}
	if err := (Turn{thread: thread, id: "extra"}).InvalidateRequests(); !errors.Is(err, ErrBackpressure) {
		t.Fatal("invalidation tombstones grew unbounded", err)
	}
}

func TestCanonicalApprovalRejectsDuplicateOrNullPolicyFields(t *testing.T) {
	for _, raw := range []string{`{"amendment":"one","amendment":"two"}`, `{"amendment":null}`, `null`, `["accept"]`} {
		if _, err := canonicalDecision(json.RawMessage(raw)); !errors.Is(err, ErrInvalidAnswer) {
			t.Fatalf("ambiguous decision accepted: %s %v", raw, err)
		}
	}
	one, err := canonicalDecision(json.RawMessage(`{"value":9007199254740992}`))
	if err != nil {
		t.Fatal(err)
	}
	two, err := canonicalDecision(json.RawMessage(`{"value":9007199254740993}`))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(one, two) {
		t.Fatal("approval comparison lost integer precision")
	}
}

func TestConcurrentNativeResponsesQueueExactlyOneReplyPerCallback(t *testing.T) {
	p, turn := activeFixtureTurn(t, "typed")
	question, approval := fixtureRequests(t, p, turn)
	responders := []func(int) error{
		func(i int) error { _, err := question.Answer(map[string][]string{"q1": {fmt.Sprint(i)}}); return err },
		func(i int) error {
			decision := `"decline"`
			if i%2 == 0 {
				decision = `"cancel"`
			}
			_, err := approval.Decide(json.RawMessage(decision))
			return err
		},
	}
	for _, respond := range responders {
		var wg sync.WaitGroup
		results := make(chan error, 16)
		for i := range 16 {
			wg.Add(1)
			go func(i int) { defer wg.Done(); results <- respond(i) }(i)
		}
		wg.Wait()
		close(results)
		accepted := 0
		for err := range results {
			if err == nil {
				accepted++
			} else if !errors.Is(err, ErrStaleRequest) {
				t.Fatal(err)
			}
		}
		if accepted != 1 {
			t.Fatalf("queued %d responses for one callback", accepted)
		}
	}
	var status struct{ Replies int }
	call(t, p, "fixture/status", map[string]any{}, &status)
	if status.Replies != 2 {
		t.Fatalf("native received %d answers", status.Replies)
	}
}

func TestRequestCapabilityAndUnknownMethodAreExplicit(t *testing.T) {
	config := fixtureConfig(t, "typed")
	config.Experimental = false
	p, err := Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if status, err := p.Close(); !status.TreeStopped {
			t.Errorf("fixture not stopped: %+v %v", status, err)
		}
	})
	thread, _, err := StartThread(t.Context(), p, fixtureOptions())
	if err != nil {
		t.Fatal(err)
	}
	turn, err := thread.StartText(t.Context(), "fixture", "high")
	if err != nil {
		t.Fatal(err)
	}
	nextFrame(t, p)
	call(t, p, "fixture/requests", map[string]any{}, nil)
	if _, err := turn.Request(nextFrame(t, p)); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("experimental input enabled silently: %v", err)
	}
	nextFrame(t, p)
	call(t, p, "fixture/unknown-request", map[string]any{}, nil)
	if _, err := turn.Request(nextFrame(t, p)); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unknown request interpreted as approval: %v", err)
	}
}

func TestFileApprovalSchemaDefaultsAndQuestionBlockingRemainDistinct(t *testing.T) {
	p, turn := activeFixtureTurn(t, "typed-nonblocking")
	question, _ := fixtureRequests(t, p, turn)
	if blocking, known := question.QuestionBlocking(); blocking || !known {
		t.Fatal("nonblocking question lost")
	}
	call(t, p, "fixture/file-approval", map[string]any{}, nil)
	request, err := turn.Request(nextFrame(t, p))
	if err != nil {
		t.Fatal(err)
	}
	if request.DecisionSource() != "schema_default" || len(request.Decisions()) != 4 {
		t.Fatal("schema decisions mislabeled as native advertisement")
	}
	if _, known := request.QuestionBlocking(); known {
		t.Fatal("file approval became a user question")
	}
	if _, err := request.Answer(map[string][]string{}); !errors.Is(err, ErrInvalidAnswer) {
		t.Fatal(err)
	}
	if _, err := request.Decide(json.RawMessage(`"cancel"`)); err != nil {
		t.Fatal(err)
	}
}

func TestUnobservedResolutionCannotBeRevivedByDelayedCallback(t *testing.T) {
	p, turn := activeFixtureTurn(t, "typed")
	call(t, p, "fixture/resolve", map[string]any{"requestId": 7}, nil)
	if nextFrame(t, p).Method != "serverRequest/resolved" {
		t.Fatal("missing resolution")
	}
	err := p.Call(t.Context(), "fixture/requests", map[string]any{}, nil)
	if !errors.Is(err, ErrOutcomeUnknown) || !errors.Is(p.Err(), ErrProtocol) {
		t.Fatalf("resolved request ID was reused: %v %v", err, p.Err())
	}
	if _, err := turn.Request(Frame{}); !errors.Is(err, ErrStaleRequest) {
		t.Fatal(err)
	}
	if status, err := p.Close(); !status.TreeStopped {
		t.Fatalf("invalid peer not stopped: %+v %v", status, err)
	}
}
