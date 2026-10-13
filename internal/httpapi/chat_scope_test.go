package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"coordica/internal/app"
	"coordica/internal/domain"
	"coordica/internal/mcp"
	"coordica/internal/store"
)

// Exercise the stdio tool dispatch against the real HTTP/application boundary
// without opening a listener. Discovery filtering is not authorization.
type chatBoundaryCaller struct {
	handler  http.Handler
	token    string
	statuses *[]int
}

func (c chatBoundaryCaller) Do(ctx context.Context, method, path, key string, body any) (json.RawMessage, error) {
	raw, e := json.Marshal(body)
	if e != nil {
		return nil, e
	}
	r := httptest.NewRequest(method, path, strings.NewReader(string(raw))).WithContext(ctx)
	r.Host = "127.0.0.1:8080"
	r.RemoteAddr = "127.0.0.1:12345"
	r.Header.Set("Authorization", "Bearer "+c.token)
	r.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	*c.statuses = append(*c.statuses, w.Code)
	if w.Code != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", w.Code)
	}
	return w.Body.Bytes(), nil
}

func TestNodeChatMCPDirectCallsCannotBypassQueryClosure(t *testing.T) {
	f := newBoundaryFixture(t)
	ctx := context.Background()
	owner, e := f.server.Service.Authenticate(ctx, f.owner)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.server.Service.CreateProject(ctx, owner, "project", domain.Project{Name: "Project", RepositoryURL: "https://example.com/repo", TargetBranch: "main", Checks: []domain.Check{{Name: "minimum", Command: "git", TimeoutSeconds: 1}}}); e != nil {
		t.Fatal(e)
	}
	w := boundaryRequest(f.server.RunnerHandler(), "POST", "/api/v1/runner/chat-credentials", f.node.Token, `{"name":"Shared node"}`)
	var grant domain.ChatCredentialGrant
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &grant) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	// Do not call tools/list: manually invoke even the hidden query kinds.
	for _, kind := range []string{"tasks", "blocks", "consultations", "proposals"} {
		for _, tool := range []string{"list_project_items", "get_project_item"} {
			args := fmt.Sprintf(`{"kind":%q,"limit":20}`, kind)
			if tool == "get_project_item" {
				args = fmt.Sprintf(`{"kind":%q,"id":"task_private"}`, kind)
			}
			input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{}}}` + "\n" + fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, tool, args)
			var output strings.Builder
			statuses := []int{}
			if e := (mcp.Chat{API: chatBoundaryCaller{f.server.RunnerHandler(), grant.Token, &statuses}}).Serve(ctx, strings.NewReader(input), &output); e != nil {
				t.Fatal(e)
			}
			if len(statuses) != 2 || statuses[0] != http.StatusOK || statuses[1] != http.StatusForbidden {
				t.Fatal("did not reach application kind closure", kind, tool, statuses)
			}
			var reply struct {
				Result struct {
					IsError bool `json:"isError"`
				} `json:"result"`
			}
			lines := strings.Split(strings.TrimSpace(output.String()), "\n")
			if len(lines) != 2 || json.Unmarshal([]byte(lines[1]), &reply) != nil || !reply.Result.IsError {
				t.Fatal("MCP bypass accepted", kind, tool, output.String())
			}
		}
	}
}

func TestHostChatAudienceIssuanceAndDirectBypasses(t *testing.T) {
	f := newBoundaryFixture(t)
	ctx := context.Background()
	owner, e := f.server.Service.Authenticate(ctx, f.owner)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.server.Service.CreateProject(ctx, owner, "project", domain.Project{Name: "Shared", RepositoryURL: "https://example.com/repo", TargetBranch: "main", Checks: []domain.Check{{Name: "minimum", Command: "git", TimeoutSeconds: 1}}}); e != nil {
		t.Fatal("project cap still required", e)
	}
	raw, e := f.server.Service.CreateChatCredential(ctx, owner, "center", domain.ChatCredentialRequest{Name: "Center host"})
	if e != nil {
		t.Fatal(e)
	}
	var center domain.ChatCredentialGrant
	if e = json.Unmarshal(raw, &center); e != nil {
		t.Fatal(e)
	}
	nodes := []domain.RegisterResponse{f.node, f.register(t, "Second host")}
	if e = f.server.Service.DB.Update(ctx, func(tx *store.Tx) error {
		if e := tx.Put("task", "task_private", domain.Task{ID: "task_private", Title: "global private task", Description: "global private description", CurrentRunID: "run_second"}); e != nil {
			return e
		}
		if e := tx.Put("decision", "decision_private", domain.Decision{ID: "decision_private", Status: "open", Evidence: "private Owner decision"}); e != nil {
			return e
		}
		for i, id := range []string{"run_first", "run_second"} {
			if e := tx.Put("run", id, domain.Run{ID: id, TaskID: "task_private", NodeID: nodes[i].NodeID, State: "running"}); e != nil {
				return e
			}
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	for _, node := range nodes {
		w := boundaryRequest(f.server.RunnerHandler(), "POST", "/api/v1/runner/chat-credentials", node.Token, `{"name":"Shared host"}`)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var grant domain.ChatCredentialGrant
		if e = json.Unmarshal(w.Body.Bytes(), &grant); e != nil {
			t.Fatal(e)
		}
		if grant.Credential.ChatScope != domain.ChatScopeNode || grant.Credential.NodeID != node.NodeID {
			t.Fatal("issuance scope not server bound", grant.Credential)
		}
		for _, h := range []http.Handler{f.server.RunnerHandler(), f.server.Handler()} {
			for _, path := range []string{"overview", "identity"} {
				if w = boundaryRequest(h, "GET", "/api/v1/chat/"+path, grant.Token, ""); w.Code != 200 {
					t.Fatal("node facts unavailable", w.Code, w.Body.String())
				}
			}
			for _, kind := range []string{"requirements", "agents", "runs"} {
				if w = boundaryRequest(h, "POST", "/api/v1/chat/items", grant.Token, `{"kind":"`+kind+`","limit":1}`); w.Code != 200 {
					t.Fatal("node safe query", kind, w.Code, w.Body.String())
				}
			}
			own, other := "run_first", "run_second"
			if node.NodeID == nodes[1].NodeID {
				own, other = other, own
			}
			w = boundaryRequest(h, "POST", "/api/v1/chat/items", grant.Token, `{"kind":"runs","limit":20}`)
			var page domain.ChatPage
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &page) != nil || len(page.Items) != 1 || page.Items[0].ID != own {
				t.Fatal("cross-node list", w.Code, w.Body.String())
			}
			if w = boundaryRequest(h, "POST", "/api/v1/chat/item", grant.Token, `{"kind":"runs","id":"`+other+`"}`); w.Code != 404 {
				t.Fatal("cross-node detail", w.Code, w.Body.String())
			}
			for _, kind := range []string{"tasks", "blocks", "consultations", "proposals"} {
				for _, query := range []struct{ path, body string }{
					{"items", `{"kind":"` + kind + `","limit":20}`},
					{"item", `{"kind":"` + kind + `","id":"task_private"}`},
					{"item", `{"kind":"` + kind + `","id":"decision_private"}`},
				} {
					if w = boundaryRequest(h, "POST", "/api/v1/chat/"+query.path, grant.Token, query.body); w.Code != 403 {
						t.Fatal("node bypassed kind closure", query.path, kind, w.Code, w.Body.String())
					}
				}
			}
			for _, path := range []string{"consultations", "proposals", "cancel", "consultation"} {
				body := `{"expected_goal_version":0,"original_text":"forbidden","source":{"session_id":"s","message_id":"m"}}`
				if path == "cancel" {
					body = `{"id":"consult_guessed","expected_version":1}`
				}
				if path == "consultation" {
					body = `{"id":"consult_guessed"}`
				}
				if w = boundaryRequest(h, "POST", "/api/v1/chat/"+path, grant.Token, body); w.Code != 403 {
					t.Fatal("node bypassed Leader gate", path, w.Code, w.Body.String())
				}
			}
		}
		if w = boundaryRequest(f.server.AdminHandler(), "GET", "/api/v1/chat/overview", grant.Token, ""); w.Code != 403 {
			t.Fatal("node audience on admin", w.Code)
		}
		for _, field := range []string{`"node_id":"forged"`, `"chat_scope":"center"`, `"role":"owner"`} {
			if w = boundaryRequest(f.server.RunnerHandler(), "POST", "/api/v1/runner/chat-credentials", node.Token, `{"name":"Forged",`+field+`}`); w.Code != 400 {
				t.Fatal("caller-selected host scope", w.Code)
			}
		}
		body := `{"title":"Shared requirement","original_text":"src/api.go","source":{"session_id":"s","message_id":"m"}}`
		w = boundaryRequest(f.server.RunnerHandler(), "POST", "/api/v1/chat/requirements", grant.Token, body)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var req domain.Requirement
		if e = json.Unmarshal(w.Body.Bytes(), &req); e != nil || req.SourceNodeID != node.NodeID || req.Source.ClientID != grant.Credential.ID {
			t.Fatal("host/chat provenance missing", req, e)
		}
		if w = boundaryRequest(f.server.RunnerHandler(), "POST", "/api/v1/chat/requirements", grant.Token, strings.TrimSuffix(body, "}")+`,"source_node_id":"forged"}`); w.Code != 400 {
			t.Fatal("spoofed source node", w.Code)
		}
	}
	if w := boundaryRequest(f.server.AdminHandler(), "POST", "/api/v1/project/budget", f.owner, `{}`); w.Code != 409 || !strings.Contains(w.Body.String(), `"code":"deprecated"`) {
		t.Fatal("legacy endpoint implies an effective cap", w.Code, w.Body.String())
	}
	// Every remote chat route rejects even a valid center-chat token, before
	// accepting tool inputs. IP/source strings/profile copies cannot widen it.
	for _, path := range []string{"overview", "identity", "items", "item", "requirements", "consultations", "proposals", "consultation", "cancel"} {
		method := "POST"
		if path == "overview" || path == "identity" {
			method = "GET"
		}
		w := boundaryRequest(f.server.RunnerHandler(), method, "/api/v1/chat/"+path, center.Token, `{}`)
		if w.Code != 403 {
			t.Fatal("center token on remote audience", path, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"state", "export", "planning/consult", "chat-credentials", "project"} {
		if w := boundaryRequest(f.server.RunnerHandler(), "POST", "/api/v1/"+path, f.owner, `{}`); w.Code != 404 {
			t.Fatal("remote Owner route", path, w.Code)
		}
	}
	// Direct application calls are independently scoped, even if a transport is
	// accidentally broadened or a caller forges Actor metadata.
	a, e := f.server.Service.Authenticate(ctx, nodes[0].Token)
	if e != nil {
		t.Fatal(e)
	}
	raw, e = f.server.Service.CreateNodeChatCredential(ctx, a, "app-grant", domain.ChatCredentialRequest{Name: "Host"})
	if e != nil {
		t.Fatal(e)
	}
	var g domain.ChatCredentialGrant
	_ = json.Unmarshal(raw, &g)
	forged := app.Actor{ID: g.Credential.ID, Role: "chat", ChatScope: domain.ChatScopeCenter}
	if _, e = f.server.Service.ConsultLeader(ctx, forged, "bypass", "consultation", domain.ConversationInput{OriginalText: "bypass", Source: domain.ChatSource{SessionID: "s", MessageID: "m"}}); e == nil {
		t.Fatal("application trusted transport scope")
	}
}
