package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"coordica/internal/apiclient"
	"coordica/internal/domain"
	"coordica/internal/mcp"
)

func workProtocolCall(t *testing.T, caller mcp.Caller, name string, input any, rejected bool) json.RawMessage {
	t.Helper()
	call, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": name, "arguments": input}})
	lines := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","clientInfo":{"name":"new-session","version":"1"}}}` + "\n" + string(call) + "\n"
	var output strings.Builder
	if err := (mcp.Interaction{Chat: mcp.Chat{API: caller}}).Serve(context.Background(), strings.NewReader(lines), &output); err != nil {
		t.Fatal(err)
	}
	responses := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(responses) != 2 {
		t.Fatal("bad stdio protocol", output.String())
	}
	var response struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if e := json.Unmarshal([]byte(responses[1]), &response); e != nil || response.Result.IsError != rejected || len(response.Result.Content) != 1 {
		t.Fatal("unexpected tool response", output.String(), e)
	}
	return json.RawMessage(response.Result.Content[0].Text)
}

func TestWorkUnifiedMCPReconnectOverActualHTTP(t *testing.T) {
	f := newBoundaryFixture(t)
	ctx := context.Background()
	owner, e := f.server.Service.Authenticate(ctx, f.owner)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := f.server.Service.CreateProject(ctx, owner, "p", domain.Project{Name: "Protocol", RepositoryURL: "https://example.test/repo", TargetBranch: "main", Checks: []domain.Check{{Name: "minimum", Command: "git", TimeoutSeconds: 1}}})
	if e != nil {
		t.Fatal(e)
	}
	var p domain.Project
	_ = json.Unmarshal(raw, &p)
	raw, e = f.server.Service.CreateChatCredential(ctx, owner, "chat", domain.ChatCredentialRequest{Name: "Shared"})
	if e != nil {
		t.Fatal(e)
	}
	var grant domain.ChatCredentialGrant
	_ = json.Unmarshal(raw, &grant)
	server := httptest.NewServer(f.server.AdminHandler())
	defer server.Close()
	client, e := apiclient.New(server.URL, grant.Token, "")
	if e != nil {
		t.Fatal(e)
	}
	defer client.CloseIdleConnections()
	anchor := domain.WorkAnchor{Kind: "project", ID: p.ID}
	var view domain.WorkContext
	if e = json.Unmarshal(workProtocolCall(t, client, "get_work_context", domain.WorkContextQuery{Anchor: anchor, Limit: 10}, false), &view); e != nil {
		t.Fatal(e)
	}
	original := strings.Repeat("<", 15000) // encoding/json escapes this beyond 32 KiB at both protocol boundaries.
	in := map[string]any{"anchor": anchor, "expected_stamp": view.Current.Reference.Stamp, "kind": "note", "original_text": original, "references": []domain.WorkReference{}, "source": domain.ChatSource{SessionID: "first_session", MessageID: "original"}, "idempotency_key": "stable"}
	workProtocolCall(t, client, "record_work_note", in, false)
	// Each call constructs a fresh unified MCP stdio session on the same profile.
	if e = json.Unmarshal(workProtocolCall(t, client, "get_work_context", domain.WorkContextQuery{Anchor: anchor, Limit: 10}, false), &view); e != nil || len(view.Notes) != 1 {
		t.Fatal("reconnect lost record", view, e)
	}
	if view.Notes[0].OriginalText != original {
		t.Fatal("escaped original was truncated or modified")
	}
	if e = json.Unmarshal(workProtocolCall(t, client, "get_work_context", domain.WorkContextQuery{Anchor: anchor, Limit: 10}, false), &view); e != nil {
		t.Fatal(e)
	}
	snapshot, e := f.server.Service.Snapshot(ctx, owner)
	if e != nil {
		t.Fatal(e)
	}
	if !snapshot.Project.Paused || len(snapshot.Runs) != 0 || len(snapshot.Tasks) != 0 || len(snapshot.PlanningRuns) != 0 {
		t.Fatal("query/note dispatched work")
	}
	if _, e = f.server.Service.RevokeChatCredential(ctx, owner, "revoke", grant.Credential.ID); e != nil {
		t.Fatal(e)
	}
	workProtocolCall(t, client, "record_work_note", in, true)
}
