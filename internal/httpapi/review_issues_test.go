package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestReviewIssueRoutesBoundScopePaginationAndMutations(t *testing.T) {
	f := newBoundaryFixture(t)
	if err := f.server.Service.DB.Update(context.Background(), func(tx *store.Tx) error {
		for _, id := range []string{"issue_a", "issue_b", "issue_foreign"} {
			task := "task_one"
			if id == "issue_foreign" {
				task = "task_other"
			}
			if err := tx.Put("review_issue", id, domain.ReviewIssue{ID: id, TaskID: task, Version: 1, Source: "internal", State: "open"}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/review-issues?task_id=task_one&limit=1"
	w := boundaryRequest(f.server.AdminHandler(), "GET", path, f.owner, "")
	var page domain.ReviewIssuePage
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || w.Code != 200 || len(page.Items) != 1 || page.Items[0].ID != "issue_a" || page.Next != "issue_a" {
		t.Fatal(w.Code, w.Body.String(), err)
	}
	w = boundaryRequest(f.server.AdminHandler(), "GET", path+"&after="+page.Next, f.owner, "")
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || w.Code != 200 || len(page.Items) != 1 || page.Items[0].ID != "issue_b" || page.Next != "" {
		t.Fatal(w.Code, w.Body.String(), err)
	}
	for _, extra := range []string{"&limit=2", "&unknown=true", "&after=a&after=b"} {
		if w := boundaryRequest(f.server.AdminHandler(), "GET", path+extra, f.owner, ""); w.Code != 400 {
			t.Fatal("ambiguous query accepted", extra, w.Code)
		}
	}
	for _, methodPath := range [][2]string{{"GET", path}, {"POST", "/api/v1/review-issues/issue_a/resolve"}} {
		if w := boundaryRequest(f.server.AdminHandler(), methodPath[0], methodPath[1], f.node.Token, `{}`); w.Code != 403 {
			t.Fatal("node reached Owner issue API", w.Code)
		}
		if w := boundaryRequest(f.server.RunnerHandler(), methodPath[0], methodPath[1], f.owner, `{}`); w.Code != http.StatusNotFound && w.Code != 403 {
			t.Fatal("remote listener exposed Owner issue API", w.Code)
		}
	}
}
