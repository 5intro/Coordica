package httpapi

import (
	"net/http"
	"strconv"

	"coordica/internal/app"
	"coordica/internal/domain"
)

// Registered only on admin/shared loopback. The remote Runner and limited chat
// surfaces cannot edit acceptance policy, test definitions or supplementary work.
func registerTestCatalog(mux *http.ServeMux, s *Server) {
	registerAcceptanceSignoffs(mux, s)
	catalog := app.TestCatalog{Service: s.Service}
	mux.HandleFunc("GET /api/v1/tests/acceptance/plan", s.auth("owner", func(w http.ResponseWriter, r *http.Request) {
		if err := testQueryKeys(r, "requirement_id"); err != nil {
			respond(w, nil, err)
			return
		}
		value, err := catalog.ReadAcceptance(r.Context(), r.Context().Value(actorKey{}).(app.Actor), r.URL.Query().Get("requirement_id"))
		respond(w, value, err)
	}))
	mux.HandleFunc("GET /api/v1/tests/cases", s.auth("owner", func(w http.ResponseWriter, r *http.Request) {
		q, err := testCaseQuery(r)
		if err != nil {
			respond(w, nil, err)
			return
		}
		page, err := catalog.List(r.Context(), r.Context().Value(actorKey{}).(app.Actor), q)
		respond(w, page, err)
	}))
	mux.HandleFunc("POST /api/v1/tests/cases", s.auth("owner", func(w http.ResponseWriter, r *http.Request) {
		var in domain.TestCaseSave
		if err := decode(r, &in); err != nil {
			respond(w, nil, err)
			return
		}
		value, err := catalog.Save(r.Context(), r.Context().Value(actorKey{}).(app.Actor), r.Header.Get("Idempotency-Key"), in)
		respond(w, value, err)
	}))
	mux.HandleFunc("POST /api/v1/tests/acceptance", s.auth("owner", func(w http.ResponseWriter, r *http.Request) {
		var in domain.AcceptanceSave
		if err := decode(r, &in); err != nil {
			respond(w, nil, err)
			return
		}
		value, err := catalog.SaveAcceptance(r.Context(), r.Context().Value(actorKey{}).(app.Actor), r.Header.Get("Idempotency-Key"), in)
		respond(w, value, err)
	}))
	mux.HandleFunc("GET /api/v1/tests/acceptance", s.auth("owner", func(w http.ResponseWriter, r *http.Request) {
		if err := testQueryKeys(r, "requirement_id", "commit"); err != nil {
			respond(w, nil, err)
			return
		}
		value, err := catalog.Coverage(r.Context(), r.Context().Value(actorKey{}).(app.Actor),
			r.URL.Query().Get("requirement_id"), r.URL.Query().Get("commit"))
		respond(w, value, err)
	}))
	mux.HandleFunc("POST /api/v1/tests/supplements", s.auth("owner", func(w http.ResponseWriter, r *http.Request) {
		var in domain.TestSupplementRequest
		if err := decode(r, &in); err != nil {
			respond(w, nil, err)
			return
		}
		value, err := catalog.Supplement(r.Context(), r.Context().Value(actorKey{}).(app.Actor), r.Header.Get("Idempotency-Key"), in)
		respond(w, value, err)
	}))
}

func testQueryKeys(r *http.Request, allowed ...string) error {
	for key, values := range r.URL.Query() {
		known := false
		for _, value := range allowed {
			known = known || key == value
		}
		if !known || len(values) != 1 {
			return domain.Fail("invalid_input", "未知或重复的测试查询条件")
		}
	}
	return nil
}

func testCaseQuery(r *http.Request) (domain.TestCaseQuery, error) {
	q := domain.TestCaseQuery{Limit: 50, After: r.URL.Query().Get("after"),
		Category: r.URL.Query().Get("category"), RequirementID: r.URL.Query().Get("requirement_id")}
	if err := testQueryKeys(r, "limit", "after", "category", "requirement_id"); err != nil {
		return q, err
	}
	if raw, ok := r.URL.Query()["limit"]; ok {
		var err error
		q.Limit, err = strconv.Atoi(raw[0])
		if err != nil {
			return q, domain.Fail("invalid_input", "无效的测试列表页大小")
		}
	}
	return q, domain.ValidateTestCaseQuery(q)
}
