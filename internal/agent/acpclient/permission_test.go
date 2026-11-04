package acpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

type permissionTurnOutcome struct {
	result PromptResult
	err    error
}

func permissionFixture(t *testing.T, scenario string) (*Peer, Session, string) {
	t.Helper()
	config, journal := fixtureConfig(t, "permission-"+scenario)
	p, err := Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	s, err := p.NewSession(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return p, s, journal
}

func startPermissionTurn(s Session, cancellation <-chan struct{}) <-chan permissionTurnOutcome {
	done := make(chan permissionTurnOutcome, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		result, err := s.PromptTextWithCancel(ctx, "explicit-permission-work", cancellation)
		done <- permissionTurnOutcome{result, err}
	}()
	return done
}

func awaitPermissionTurn(t *testing.T, done <-chan permissionTurnOutcome) permissionTurnOutcome {
	t.Helper()
	select {
	case outcome := <-done:
		return outcome
	case <-time.After(12 * time.Second):
		t.Fatal("owned permission Prompt did not finish and reap writers")
		return permissionTurnOutcome{}
	}
}

func awaitPermissions(t *testing.T, s Session, count int) []PermissionRequest {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		requests, err := s.PermissionRequests()
		if err != nil {
			t.Fatal("permission capture failed", err)
		}
		if len(requests) == count {
			return requests
		}
		if len(requests) > count || time.Now().After(deadline) {
			t.Fatalf("permission request count=%d, want %d", len(requests), count)
		}
		time.Sleep(time.Millisecond)
	}
}

type permissionWireReply struct {
	ID     json.RawMessage `json:"id"`
	Result struct {
		Outcome map[string]json.RawMessage `json:"outcome"`
	} `json:"result"`
}

func TestPermissionOpaqueOptionUsesOriginalNativeJSONValue(t *testing.T) {
	_, s, journal := permissionFixture(t, "opaque-option")
	done := startPermissionTurn(s, nil)
	request := awaitPermissions(t, s, 1)[0]
	if _, err := s.SelectPermission(t.Context(), request, request.Options()[0].ID); err != nil {
		t.Fatal(err)
	}
	if out := awaitPermissionTurn(t, done); out.err != nil || out.result.StopReason != "end_turn" {
		t.Fatal(out)
	}
	replies := permissionJournalReplies(t, journal)
	if len(replies) != 1 || string(replies[0].Result.Outcome["optionId"]) != `"\ud800"` {
		t.Fatal("decoding/re-encoding changed the native option value", replies)
	}
}

func permissionJournalReplies(t *testing.T, journal string) []permissionWireReply {
	t.Helper()
	raw, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	var replies []permissionWireReply
	lines := bytes.Split(raw, []byte{'\n'})
	for _, line := range lines[:len(lines)-1] {
		var envelope map[string]json.RawMessage
		if json.Unmarshal(line, &envelope) != nil {
			t.Fatal("invalid owned-child journal frame")
		}
		if _, request := envelope["method"]; request {
			continue
		}
		var reply permissionWireReply
		if json.Unmarshal(line, &reply) != nil || reply.Result.Outcome == nil {
			t.Fatal("client emitted an unexpected non-request frame")
		}
		replies = append(replies, reply)
	}
	return replies
}

func TestPermissionExplicitSelectionKeepsFullTerminalAndPrivateCopies(t *testing.T) {
	p, s, journal := permissionFixture(t, "normal")
	done := startPermissionTurn(s, nil)
	requests := awaitPermissions(t, s, 1)
	request := requests[0]
	if len(request.Options()) != 4 || !bytes.Contains(request.ToolCall(), []byte("private-native-command")) {
		t.Fatal("private native choices/details were not exposed to the trusted caller")
	}
	select {
	case outcome := <-done:
		t.Fatal("permission request was mistaken for a Prompt terminal", outcome)
	default:
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.SelectPermission(ctx, request, "permit::opaque"); !errors.Is(err, context.Canceled) {
		t.Fatal("pre-cancelled selection sent or consumed request", err)
	}
	for _, invalid := range []string{"allow_once", "private-native-label", ""} {
		if _, err := s.SelectPermission(t.Context(), request, invalid); !errors.Is(err, ErrInvalidPermissionAnswer) {
			t.Fatal("non-native choice was mapped or consumed request", err)
		}
	}
	if len(permissionJournalReplies(t, journal)) != 0 || len(awaitPermissions(t, s, 1)) != 1 {
		t.Fatal("permission was automatically answered")
	}
	id, tool, options := request.ID(), request.ToolCall(), request.Options()
	id[0], tool[0], options[0].ID = 'X', 'X', "changed"
	options[0].rawID[0] = 'X'
	requests[0].id[0], requests[0].toolCall[0], requests[0].options[0].ID = 'Y', 'Y', "changed-again"
	requests[0].options[0].rawID[0] = 'Y'
	request = awaitPermissions(t, s, 1)[0]
	if request.ID()[0] != '"' || request.ToolCall()[0] != '{' || request.Options()[0].ID != "permit::opaque" || string(request.Options()[0].rawID) != `"permit::opaque"` {
		t.Fatal("private accessor or inbox snapshot storage was mutable")
	}
	receipt, err := s.SelectPermission(t.Context(), request, "deny::opaque")
	if err != nil || !receipt.Written || receipt.LocalRequest != 1 || receipt.Evidence != "local_write" || receipt.Outcome != "selected" {
		t.Fatal("explicit reply lost local-only transport evidence", receipt, err)
	}
	outcome := awaitPermissionTurn(t, done)
	if outcome.err != nil || outcome.result.Evidence != "rpc_terminal" || outcome.result.StopReason != "end_turn" ||
		outcome.result.LocalRequestID != 3 || p.Err() != nil {
		t.Fatal("native full Prompt terminal was not retained", outcome, p.Err())
	}
	replies := permissionJournalReplies(t, journal)
	if len(replies) != 1 || string(replies[0].ID) != `"permission-1"` ||
		string(replies[0].Result.Outcome["outcome"]) != `"selected"` ||
		string(replies[0].Result.Outcome["optionId"]) != `"deny::opaque"` {
		t.Fatal("native exact request/option forwarding changed", replies)
	}
	updates, _ := s.Updates()
	metadata, _ := json.Marshal([]any{receipt, outcome.result, updates})
	if bytes.Contains(metadata, []byte("private-")) {
		t.Fatal("private permission body entered ordinary metadata")
	}
}

func TestPermissionClosedCancellationSignalCannotSelectAnOption(t *testing.T) {
	p, s, journal := permissionFixture(t, "normal")
	for i := 0; i < 10; i++ {
		signal := make(chan struct{})
		done := startPermissionTurn(s, signal)
		request := awaitPermissions(t, s, 1)[0]
		close(signal)
		if _, err := s.SelectPermission(t.Context(), request, "permit::opaque"); !errors.Is(err, ErrStalePermission) {
			t.Fatal("closed cancellation signal still selected an option", err)
		}
		out := awaitPermissionTurn(t, done)
		if out.err != nil || out.result.StopReason != "cancelled" || p.Err() != nil {
			t.Fatal(out, p.Err())
		}
	}
	replies := permissionJournalReplies(t, journal)
	if len(replies) != 10 {
		t.Fatal("cancel signal was consumed without answering the native request", len(replies))
	}
	for _, reply := range replies {
		if len(reply.Result.Outcome) != 1 || string(reply.Result.Outcome["outcome"]) != `"cancelled"` {
			t.Fatal("cancel became an approval", reply)
		}
	}
}

func TestPermissionConcurrentOppositeAnswersWriteAtMostOnce(t *testing.T) {
	_, s, journal := permissionFixture(t, "normal")
	done := startPermissionTurn(s, nil)
	request := awaitPermissions(t, s, 1)[0]
	type answer struct {
		choice  string
		receipt PermissionReceipt
		err     error
	}
	start, answers := make(chan struct{}), make(chan answer, 2)
	for _, choice := range []string{"permit::opaque", "deny::opaque"} {
		go func() {
			<-start
			receipt, err := s.SelectPermission(t.Context(), request, choice)
			answers <- answer{choice, receipt, err}
		}()
	}
	close(start)
	winner := ""
	for i := 0; i < 2; i++ {
		select {
		case response := <-answers:
			if response.err == nil && response.receipt.Written && winner == "" {
				winner = response.choice
			} else if !errors.Is(response.err, ErrStalePermission) {
				t.Fatal("opposite answer did not lose without send", response)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("concurrent permission selection did not return")
		}
	}
	if outcome := awaitPermissionTurn(t, done); outcome.err != nil || winner == "" {
		t.Fatal("once-only selection lost terminal/winner", outcome)
	}
	replies := permissionJournalReplies(t, journal)
	if len(replies) != 1 || string(replies[0].Result.Outcome["optionId"]) != `"`+winner+`"` {
		t.Fatal("multiple or changed native replies", replies)
	}
}

func TestPermissionNativeIDTypesStayDistinct(t *testing.T) {
	_, s, journal := permissionFixture(t, "ids")
	done := startPermissionTurn(s, nil)
	requests := awaitPermissions(t, s, 4)
	want := map[string]bool{`7`: true, `"7"`: true, `null`: true, `-7`: true}
	for _, request := range requests {
		if !want[string(request.ID())] {
			t.Fatal("native id type/value was changed", string(request.ID()))
		}
		if receipt, err := s.SelectPermission(t.Context(), request, "remember::R"); err != nil || !receipt.Written {
			t.Fatal("typed request could not be explicitly answered", receipt, err)
		}
	}
	if outcome := awaitPermissionTurn(t, done); outcome.err != nil || outcome.result.StopReason != "end_turn" {
		t.Fatal(outcome)
	}
	replies := permissionJournalReplies(t, journal)
	if len(replies) != 4 {
		t.Fatal("typed callback response count", len(replies))
	}
	for _, reply := range replies {
		if !want[string(reply.ID)] || string(reply.Result.Outcome["optionId"]) != `"remember::R"` {
			t.Fatal("response did not preserve original typed id/choice", reply)
		}
		delete(want, string(reply.ID))
	}
}

func TestPermissionHandlesRejectOtherBindingsAndEarlierPrompt(t *testing.T) {
	_, s, journal := permissionFixture(t, "normal")
	done := startPermissionTurn(s, nil)
	request := awaitPermissions(t, s, 1)[0]
	_, other, _ := permissionFixture(t, "normal")
	changedSession, changedEpoch := s, s
	changedSession.id, changedEpoch.epoch = "other-session", "other-epoch"
	for _, caller := range []Session{{}, other, changedSession, changedEpoch} {
		if _, err := caller.SelectPermission(t.Context(), request, "permit::opaque"); !errors.Is(err, ErrStalePermission) {
			t.Fatal("foreign Session/Peer/epoch accepted request", err)
		}
	}
	if _, err := s.SelectPermission(t.Context(), PermissionRequest{}, "permit::opaque"); !errors.Is(err, ErrStalePermission) {
		t.Fatal("zero request handle was accepted", err)
	}
	if _, err := s.SelectPermission(t.Context(), request, "permit::opaque"); err != nil {
		t.Fatal(err)
	}
	if outcome := awaitPermissionTurn(t, done); outcome.err != nil {
		t.Fatal(outcome)
	}
	done = startPermissionTurn(s, nil)
	next := awaitPermissions(t, s, 1)[0]
	if _, err := s.SelectPermission(t.Context(), request, "deny::opaque"); !errors.Is(err, ErrStalePermission) {
		t.Fatal("earlier Prompt handle targeted later work", err)
	}
	if _, err := s.SelectPermission(t.Context(), next, "remember::A"); err != nil {
		t.Fatal(err)
	}
	if outcome := awaitPermissionTurn(t, done); outcome.err != nil || outcome.result.LocalRequestID != 4 {
		t.Fatal("second explicit Prompt failed", outcome)
	}
	if replies := permissionJournalReplies(t, journal); len(replies) != 2 || string(replies[1].ID) != `"permission-2"` {
		t.Fatal("old native request was replayed into next Prompt", replies)
	}
}

func TestPermissionNativeCancelAnswersEmptyAndLateRequests(t *testing.T) {
	for _, scenario := range []string{"empty", "cancel-late"} {
		t.Run(scenario, func(t *testing.T) {
			p, s, journal := permissionFixture(t, scenario)
			signal := make(chan struct{})
			done := startPermissionTurn(s, signal)
			count := 1
			if scenario == "cancel-late" {
				count = 2
			}
			requests := awaitPermissions(t, s, count)
			if scenario == "empty" {
				if len(requests[0].Options()) != 0 {
					t.Fatal("empty options acquired a default choice")
				}
				if _, err := s.SelectPermission(t.Context(), requests[0], ""); !errors.Is(err, ErrInvalidPermissionAnswer) {
					t.Fatal("empty options allowed a selection", err)
				}
			}
			if len(permissionJournalReplies(t, journal)) != 0 {
				t.Fatal("pending request was answered before explicit cancellation")
			}
			close(signal)
			outcome := awaitPermissionTurn(t, done)
			if outcome.err != nil || outcome.result.StopReason != "cancelled" || p.Err() != nil {
				t.Fatal("cancelled callbacks lost original native terminal", outcome, p.Err())
			}
			replies := permissionJournalReplies(t, journal)
			if scenario == "cancel-late" {
				count++
			}
			if len(replies) != count {
				t.Fatal("not every pending/late request was cancelled", len(replies), count)
			}
			for _, reply := range replies {
				if len(reply.Result.Outcome) != 1 || string(reply.Result.Outcome["outcome"]) != `"cancelled"` {
					t.Fatal("cancelled answer contained selection or invented option", reply)
				}
			}
			if _, err := s.SelectPermission(t.Context(), requests[0], "permit::opaque"); !errors.Is(err, ErrStalePermission) {
				t.Fatal("cancelled request remained answerable", err)
			}
		})
	}
}

func assertUnknownPermissionTurn(t *testing.T, p *Peer, s Session, outcome permissionTurnOutcome) {
	t.Helper()
	var failure *PromptError
	if outcome.result.Evidence != "" || !errors.Is(outcome.err, ErrOutcomeUnknown) ||
		!errors.As(outcome.err, &failure) || !failure.Process.TreeStopped {
		t.Fatal("unknown permission work lost actual cleanup evidence", outcome)
	}
	if strings.Contains(outcome.err.Error(), "private-") {
		t.Fatal("private callback body entered error text")
	}
	if _, err := s.PromptText(t.Context(), "must-not-retry"); !errors.Is(err, ErrDisconnected) {
		t.Fatal("retired Peer accepted replacement work", err)
	}
	if status, _ := p.Close(); !status.TreeStopped {
		t.Fatal("permission failure did not actually stop the owned child")
	}
}

func TestPermissionInvalidAndOverBudgetCallbacksFailClosed(t *testing.T) {
	for _, scenario := range []string{"wrong-session", "malformed", "duplicate", "flood", "large", "budget"} {
		t.Run(scenario, func(t *testing.T) {
			p, s, journal := permissionFixture(t, scenario)
			outcome := awaitPermissionTurn(t, startPermissionTurn(s, nil))
			assertUnknownPermissionTurn(t, p, s, outcome)
			if len(permissionJournalReplies(t, journal)) != 0 {
				t.Fatal("invalid callback caused an automatic native answer")
			}
			if scenario == "flood" || scenario == "budget" {
				if !errors.Is(outcome.err, ErrBackpressure) {
					t.Fatal("bounded capture failure lost backpressure distinction", outcome.err)
				}
			}
		})
	}
}

func TestPermissionPendingAtTerminalRetainsNativeFactAndRetiresPeer(t *testing.T) {
	p, s, journal := permissionFixture(t, "pending-terminal")
	outcome := awaitPermissionTurn(t, startPermissionTurn(s, nil))
	var failure *PromptError
	if outcome.result.Evidence != "rpc_terminal" || outcome.result.StopReason != "end_turn" ||
		!errors.Is(outcome.err, ErrPermissionUnresolved) || errors.Is(outcome.err, ErrOutcomeUnknown) ||
		!errors.As(outcome.err, &failure) || !failure.Process.TreeStopped || p.Err() == nil {
		t.Fatal("orphan permission erased known terminal or left Peer reusable", outcome)
	}
	if len(permissionJournalReplies(t, journal)) != 0 {
		t.Fatal("natural terminal synthesized a permission answer")
	}
}

func TestPermissionAnsweredIDReuseIsRejected(t *testing.T) {
	p, s, journal := permissionFixture(t, "reuse")
	done := startPermissionTurn(s, nil)
	request := awaitPermissions(t, s, 1)[0]
	receipt, err := s.SelectPermission(t.Context(), request, "permit::opaque")
	if !receipt.Written || (err != nil && !errors.Is(err, ErrDisconnected)) {
		t.Fatal("first local response lost before duplicate callback", receipt, err)
	}
	assertUnknownPermissionTurn(t, p, s, awaitPermissionTurn(t, done))
	if replies := permissionJournalReplies(t, journal); len(replies) != 1 {
		t.Fatal("already answered native id was answered again", len(replies))
	}
}

func TestPermissionLifetimeTombstoneLimitDoesNotResetAfterAnswers(t *testing.T) {
	p, s, journal := permissionFixture(t, "lifetime")
	done := startPermissionTurn(s, nil)
	for i := 1; i <= 64; i++ {
		request := awaitPermissions(t, s, 1)[0]
		receipt, err := s.SelectPermission(t.Context(), request, "deny::opaque")
		if !receipt.Written || receipt.LocalRequest != uint64(i) || (err != nil && (i != 64 || !errors.Is(err, ErrDisconnected))) {
			t.Fatal("explicit bounded-lifetime answer failed", i, receipt, err)
		}
	}
	outcome := awaitPermissionTurn(t, done)
	assertUnknownPermissionTurn(t, p, s, outcome)
	if !errors.Is(outcome.err, ErrBackpressure) || len(permissionJournalReplies(t, journal)) != 64 {
		t.Fatal("64 answered tombstones did not fence the 65th callback", outcome.err)
	}
}
