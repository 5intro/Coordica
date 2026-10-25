package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"coordica/internal/app"
	"coordica/internal/domain"
	"coordica/internal/mcp"
)

type preparationBridge struct {
	fixture *boundaryFixture
	chat    domain.ChatCredentialGrant
}

func newPreparationBridge(t *testing.T) preparationBridge {
	t.Helper()
	f := newBoundaryFixture(t)
	f.server.Service.Source = fixtureUnbornSource{}
	project := domain.Project{
		Name: "Preparation bridge", RepositoryURL: "https://example.com/repo", TargetBranch: "main",
		Checks: []domain.Check{{Name: "tests", Command: "go", Args: []string{"test", "./..."}, TimeoutSeconds: 30}},
	}
	body, err := json.Marshal(project)
	if err != nil {
		t.Fatal(err)
	}
	w := boundaryRequest(f.server.AdminHandler(), "POST", "/api/v1/project", f.owner, string(body))
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	w = boundaryRequest(f.server.AdminHandler(), "POST", "/api/v1/chat-credentials", f.owner, `{"name":"Center conversation"}`)
	var chat domain.ChatCredentialGrant
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &chat) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	return preparationBridge{fixture: f, chat: chat}
}

func preparationMCP(t *testing.T, handler http.Handler, token, name string, args any) (json.RawMessage, bool) {
	t.Helper()
	call := map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args}}
	raw, err := json.Marshal(call)
	if err != nil {
		t.Fatal(err)
	}
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}` + "\n" + string(raw)
	var output strings.Builder
	statuses := []int{}
	api := chatBoundaryCaller{handler: handler, token: token, statuses: &statuses}
	if err = (mcp.Interaction{Chat: mcp.Chat{API: api}}).Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	var reply struct {
		Error  json.RawMessage `json:"error"`
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if len(lines) != 2 || json.Unmarshal([]byte(lines[1]), &reply) != nil {
		t.Fatal("invalid MCP reply", output.String())
	}
	if len(reply.Error) > 0 {
		return reply.Error, true
	}
	if len(reply.Result.Content) != 1 {
		t.Fatal("invalid MCP content", output.String())
	}
	return json.RawMessage(reply.Result.Content[0].Text), reply.Result.IsError
}

func (b preparationBridge) call(t *testing.T, name string, args any) domain.Initialization {
	t.Helper()
	raw, rejected := preparationMCP(t, b.fixture.server.AdminHandler(), b.chat.Token, name, args)
	var value domain.Initialization
	if rejected || json.Unmarshal(raw, &value) != nil {
		t.Fatal("MCP initialization call rejected", name, string(raw))
	}
	return value
}

func (b preparationBridge) read(t *testing.T) domain.Initialization {
	t.Helper()
	w := boundaryRequest(b.fixture.server.AdminHandler(), "GET", "/api/v1/initialization", b.fixture.owner, "")
	var value domain.Initialization
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &value) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	return value
}

func (b preparationBridge) analyze(t *testing.T, ready bool) domain.Initialization {
	t.Helper()
	s := b.fixture.server.Service
	p, err := s.Project(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	v := b.read(t)
	baseline := domain.Baseline{
		Requirements: "Shared requirements", Acceptance: "Observable behavior", Architecture: "Modular monolith",
		Modules: "Single-owner modules", Conventions: "Small cohesive functions", Testing: "Failure-mode regressions",
		Functionality: "First iteration", Risks: "Unverified platforms", AllowedRoots: []string{"."},
		Checks: p.Checks, ProtectedPaths: p.ProtectedPaths,
	}
	questions := []domain.InitializationQuestion{}
	if !ready {
		question := domain.InitializationQuestion{ID: "purpose", Text: "Who is the first user?", Prerequisites: []string{}}
		if len(v.Feedback) > 0 {
			question = domain.InitializationQuestion{ID: "extension", Text: "Is later expansion needed?", Prerequisites: []string{"purpose"}}
		}
		questions = append(questions, question)
	}
	result := domain.InitializationResult{
		Generation: v.OperationGeneration, InitializationVersion: v.Version, OperationID: v.OperationID,
		SourceSHA: v.SourceSHA, Ready: ready, InspectedPaths: []string{}, Facts: []domain.AnalysisEvidence{},
		Suggestions: []string{}, Questions: questions, Baseline: baseline,
	}
	w := app.InitializationWorker{Service: s, Executor: fixtureBaselineExecutor{Result: result}}
	if err = w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	value := b.read(t)
	if value.Status != "interview" && value.Status != "awaiting_owner" {
		t.Fatal("fixture did not complete analysis", value.Status, value.Detail)
	}
	return value
}
