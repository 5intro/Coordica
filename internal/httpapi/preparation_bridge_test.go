package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"coordica/internal/domain"
)

func TestPreparationContinuesAcrossWebAndUnifiedMCPWithoutDispatch(t *testing.T) {
	b := newPreparationBridge(t)
	opened := b.call(t, "open_project", map[string]any{"kind": "new", "expected_version": 0, "idempotency_key": "open"})
	if opened.Status != "analyzing" || opened.Version != 1 {
		t.Fatal("unexpected open response", opened)
	}
	first := b.analyze(t, false)
	if first.Version != opened.Version || first.Result.Questions[0].ID != "purpose" {
		t.Fatal("Web did not continue the MCP interview", first)
	}
	handler := b.fixture.server.AdminHandler()
	webAnswer := `{"expected_version":1,"question_id":"purpose","original_text":"先支持个人使用",` +
		`"source":{"session_id":"owner-console","message_id":"web-answer"}}`
	w := boundaryRequest(handler, "POST", "/api/v1/initialization/answer", b.fixture.owner, webAnswer)
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	b.analyze(t, false)
	second := b.call(t, "get_project_initialization", map[string]any{})
	if second.Version != 2 || len(second.Feedback) != 1 || second.Feedback[0].OriginalText != "先支持个人使用" {
		t.Fatal("MCP did not see the Web answer", second)
	}
	chatAnswer := map[string]any{
		"expected_version": 2, "question_id": "extension", "original_text": "保留以后扩展的能力",
		"source": domain.ChatSource{SessionID: "conversation", MessageID: "chat-answer"}, "idempotency_key": "same-answer",
	}
	uncertain := b.call(t, "answer_project_initialization", chatAnswer)
	current := b.analyze(t, true)
	replay := b.call(t, "answer_project_initialization", chatAnswer)
	if replay.OperationID != uncertain.OperationID || replay.Version != uncertain.Version {
		t.Fatal("lost-receipt retry did not return the original operation", replay, uncertain)
	}
	current = b.read(t)
	if current.Version != 3 || current.Status != "awaiting_owner" || len(current.Feedback) != 2 {
		t.Fatal("retry advanced the interview twice", current)
	}
	if current.Feedback[1].Source.ClientID != b.chat.Credential.ID || current.Feedback[0].OriginalText != "先支持个人使用" {
		t.Fatal("original provenance changed", current.Feedback)
	}
	staleAnswer := strings.Replace(webAnswer, "web-answer", "new-stale-message", 1)
	if stale := boundaryRequest(handler, "POST", "/api/v1/initialization/answer", b.fixture.owner, staleAnswer); stale.Code != 409 {
		t.Fatal("old Web answer advanced the new interview", stale.Code, stale.Body.String())
	}
	confirm := domain.InitializationConfirm{ExpectedVersion: current.Version, Digest: current.Digest}
	if _, rejected := preparationMCP(t, handler, b.chat.Token, "confirm_project_initialization", confirm); !rejected {
		t.Fatal("limited interaction gained Owner confirmation")
	}
	raw, _ := json.Marshal(confirm)
	w = boundaryRequest(handler, "POST", "/api/v1/initialization/confirm", b.fixture.owner, string(raw))
	if w.Code != http.StatusOK || b.read(t).Status != "initialized" {
		t.Fatal("Owner confirmation failed", w.Code, w.Body.String())
	}
	assertPreparationPaused(t, b)
}

func assertPreparationPaused(t *testing.T, b preparationBridge) {
	t.Helper()
	w := boundaryRequest(b.fixture.server.AdminHandler(), "GET", "/api/v1/state", b.fixture.owner, "")
	var snapshot domain.Snapshot
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &snapshot) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	if snapshot.Project == nil || !snapshot.Project.Paused || snapshot.Automation.Enabled || len(snapshot.Tasks) != 0 || len(snapshot.Runs) != 0 {
		t.Fatal("preparation implied execution authority", snapshot.Project, snapshot.Automation, snapshot.Tasks, snapshot.Runs)
	}
}

func TestPreparationRejectsNodeAccessAndRevokedChatReplay(t *testing.T) {
	b := newPreparationBridge(t)
	f := b.fixture
	w := boundaryRequest(f.server.RunnerHandler(), "POST", "/api/v1/runner/chat-credentials", f.node.Token, `{"name":"Node chat"}`)
	var node domain.ChatCredentialGrant
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &node) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, handler := range []http.Handler{f.server.AdminHandler(), f.server.RunnerHandler()} {
		if _, rejected := preparationMCP(t, handler, node.Token, "get_project_initialization", map[string]any{}); !rejected {
			t.Fatal("node interaction gained Leader initialization access")
		}
	}
	b.call(t, "open_project", map[string]any{"kind": "new", "expected_version": 0, "idempotency_key": "open"})
	b.analyze(t, false)
	answer := map[string]any{
		"expected_version": 1, "question_id": "purpose", "original_text": "保留原话",
		"source": domain.ChatSource{SessionID: "conversation", MessageID: "original"}, "idempotency_key": "original",
	}
	b.call(t, "answer_project_initialization", answer)
	w = boundaryRequest(f.server.AdminHandler(), "POST", "/api/v1/chat-credentials/"+b.chat.Credential.ID+"/revoke", f.owner, `{}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, rejected := preparationMCP(t, f.server.AdminHandler(), b.chat.Token, "answer_project_initialization", answer); !rejected {
		t.Fatal("revoked credential replayed a previously accepted answer")
	}
	if current := b.read(t); current.Version != 2 || len(current.Feedback) != 1 {
		t.Fatal("revoked replay changed the interview", current)
	}
	assertPreparationPaused(t, b)
}
