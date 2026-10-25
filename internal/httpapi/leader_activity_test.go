package httpapi

import (
	"bufio"
	"context"
	"coordica/internal/app"
	"coordica/internal/domain"
	"coordica/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLeaderActivityClosedAudienceAndBoundedQuery(t *testing.T) {
	f := newBoundaryFixture(t)
	for _, handler := range []http.Handler{f.server.AdminHandler(), f.server.Handler()} {
		for _, token := range []string{f.node.Token, "invalid"} {
			w := boundaryRequest(handler, "GET", "/api/v1/leader/activity", token, "")
			if w.Code == 200 {
				t.Fatal("node/invalid read Leader")
			}
		}
		for _, query := range []string{"limit=101", "after=-1", "after=1&after=2", "secret=x", "generation=1"} {
			if w := boundaryRequest(handler, "GET", "/api/v1/leader/activity?"+query, f.owner, ""); w.Code != 400 {
				t.Fatal(query, w.Code, w.Body.String())
			}
		}
		if w := boundaryRequest(handler, "GET", "/api/v1/leader/activity", f.owner, ""); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/api/v1/leader/activity", "/api/v1/leader/activity/stream"} {
		if w := boundaryRequest(f.server.RunnerHandler(), "GET", path, f.owner, ""); w.Code != 404 {
			t.Fatal("remote Leader exposed", w.Code)
		}
	}
	if w := boundaryRequest(f.server.RunnerHandler(), "POST", "/api/v1/chat/leader/activity", f.node.Token, `{"limit":10}`); w.Code != 404 {
		t.Fatal("remote Leader chat route exposed", w.Code)
	}
}

func TestLeaderActivityStreamWakesAndEndsWithClient(t *testing.T) {
	f := newBoundaryFixture(t)
	server := httptest.NewServer(f.server.AdminHandler())
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/v1/leader/activity/stream", nil)
	req.Header.Set("Authorization", "Bearer "+f.owner)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), 512<<10)
	next := func() string {
		t.Helper()
		for scanner.Scan() {
			if strings.HasPrefix(scanner.Text(), "data: ") {
				return scanner.Text()
			}
		}
		t.Fatal("missing SSE frame", scanner.Err())
		return ""
	}
	next()
	err = f.server.Service.DB.Update(ctx, func(tx *store.Tx) error {
		run := domain.PlanningRun{ID: app.ID("plan_"), State: "running", Generation: 1, DocumentVersion: 1}
		if err := tx.Put("planning_run", run.ID, run); err != nil {
			return err
		}
		return store.AppendLeaderActivity(tx, []domain.LeaderActivityEvent{{Execution: domain.LeaderExecutionRef{
			Kind: "planning", ID: run.ID, Generation: 1, InputVersion: 1}, Key: "one", Kind: "message", Text: "实时公开活动"}})
	})
	if err != nil {
		t.Fatal(err)
	}
	if data := next(); !strings.Contains(data, "实时公开活动") {
		t.Fatal(data)
	}
	cancel()
}
