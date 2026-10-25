package httpapi

import (
	"net/http"

	"coordica/internal/app"
	"coordica/internal/domain"
)

func registerAcceptanceSignoffs(mux *http.ServeMux, s *Server) {
	signoffs := app.AcceptanceSignoffs{Service: s.Service}
	mux.HandleFunc("GET /api/v1/tests/acceptance/signoff", s.auth("owner", func(w http.ResponseWriter, r *http.Request) {
		if err := testQueryKeys(r, "requirement_id", "commit"); err != nil {
			respond(w, nil, err)
			return
		}
		out, err := signoffs.Read(r.Context(), r.Context().Value(actorKey{}).(app.Actor),
			r.URL.Query().Get("requirement_id"), r.URL.Query().Get("commit"))
		respond(w, out, err)
	}))
	mux.HandleFunc("POST /api/v1/tests/acceptance/signoff", s.auth("owner", func(w http.ResponseWriter, r *http.Request) {
		var in domain.AcceptanceSignoffRequest
		if err := decode(r, &in); err != nil {
			respond(w, nil, err)
			return
		}
		out, err := signoffs.Decide(r.Context(), r.Context().Value(actorKey{}).(app.Actor), r.Header.Get("Idempotency-Key"), in)
		respond(w, out, err)
	}))
}
