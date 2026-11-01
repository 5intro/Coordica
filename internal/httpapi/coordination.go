package httpapi

import (
	"net/http"
	"strings"

	"coordica/internal/app"
	"coordica/internal/domain"
)

// Owner routes are mounted only on loopback; limited chat uses its existing
// authenticated listener audience and the same application aggregate.
func registerCoordination(mux *http.ServeMux, s *Server) {
	for _, path := range []string{"contexts", "context", "notes"} {
		mux.HandleFunc("POST /api/v1/work/"+path, s.auth("owner", s.workAPI))
	}
}

func (s *Server) workAPI(w http.ResponseWriter, r *http.Request) {
	c := app.Coordination{Service: s.Service}
	a := r.Context().Value(actorKey{}).(app.Actor)
	var value any
	var err error
	switch r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:] {
	case "contexts":
		var in domain.WorkListQuery
		if err = decodeChat(r, &in); err == nil {
			value, err = c.List(r.Context(), a, in)
		}
	case "context":
		var in domain.WorkContextQuery
		if err = decodeChat(r, &in); err == nil {
			value, err = c.Context(r.Context(), a, in)
		}
	case "notes":
		var in domain.WorkNoteInput
		if err = decodeChat(r, &in); err == nil {
			value, err = c.Record(r.Context(), a, r.Header.Get("Idempotency-Key"), in)
		}
	default:
		err = domain.ErrNotFound
	}
	respond(w, value, err)
}
