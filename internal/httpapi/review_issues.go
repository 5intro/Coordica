package httpapi

import (
	"coordica/internal/app"
	"coordica/internal/domain"
	"net/http"
	"strconv"
)

func registerReviewIssues(mux *http.ServeMux, s *Server) {
	issues := app.ReviewIssues{Service: s.Service}
	mux.HandleFunc("GET /api/v1/review-issues", s.auth("owner", func(w http.ResponseWriter, r *http.Request) {
		if err := testQueryKeys(r, "task_id", "after", "limit"); err != nil {
			respond(w, nil, err)
			return
		}
		limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
		if r.URL.Query().Get("limit") == "" {
			limit, err = 32, nil
		}
		if err != nil {
			respond(w, nil, domain.Fail("invalid_input", "integer issue page limit required"))
			return
		}
		out, err := issues.List(r.Context(), r.Context().Value(actorKey{}).(app.Actor), domain.ReviewIssueQuery{
			TaskID: r.URL.Query().Get("task_id"), After: r.URL.Query().Get("after"), Limit: limit})
		respond(w, out, err)
	}))
	mux.HandleFunc("POST /api/v1/review-issues/{id}/resolve", s.auth("owner", func(w http.ResponseWriter, r *http.Request) {
		var in domain.ReviewIssueDecision
		if err := decode(r, &in); err != nil {
			respond(w, nil, err)
			return
		}
		out, err := issues.Decide(r.Context(), r.Context().Value(actorKey{}).(app.Actor), r.Header.Get("Idempotency-Key"), r.PathValue("id"), in)
		respond(w, out, err)
	}))
}
