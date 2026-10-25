package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"coordica/internal/app"
	"coordica/internal/domain"
)

// Only admin/shared loopback mounts these routes. A node credential cannot
// reach Leader data, even if it guesses the closed center-chat path.
func registerLeaderActivity(mux *http.ServeMux, server *Server) {
	activities := app.LeaderActivities{Service: server.Service}
	slots := make(chan struct{}, 8)
	mux.HandleFunc("GET /api/v1/leader/activity", server.auth("owner", func(w http.ResponseWriter, r *http.Request) {
		query, err := activityQuery(r)
		if err != nil {
			respond(w, nil, err)
			return
		}
		value, err := activities.Read(r.Context(), r.Context().Value(actorKey{}).(app.Actor), query)
		respond(w, value, err)
	}))
	mux.HandleFunc("GET /api/v1/leader/activity/stream", server.auth("owner", func(w http.ResponseWriter, r *http.Request) {
		query, err := activityQuery(r)
		if err != nil {
			respond(w, nil, err)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			respond(w, nil, domain.Fail("conflict", "too many activity streams"))
			return
		}
		actor := r.Context().Value(actorKey{}).(app.Actor)
		// Validate before committing SSE headers so initial failures remain JSON.
		if _, err := activities.Read(r.Context(), actor, query); err != nil {
			respond(w, nil, err)
			return
		}
		controller := http.NewResponseController(w)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Accel-Buffering", "no")
		err = activities.Follow(r.Context(), actor, query, func(page domain.LeaderActivityPage) error {
			if err := controller.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
				return err
			}
			data, err := json.Marshal(page)
			if err != nil {
				return err
			}
			if _, err = fmt.Fprintf(w, "id: %d\nevent: activity\ndata: %s\n\n", page.Cursor, data); err != nil {
				return err
			}
			return controller.Flush()
		})
		// Revocation/errors terminate the stream; clients retain the last snapshot
		// as stale rather than treating a transport ending as execution completion.
		_ = err
	}))
	mux.HandleFunc("POST /api/v1/chat/leader/activity", server.auth("chat", func(w http.ResponseWriter, r *http.Request) {
		var query domain.LeaderActivityQuery
		err := decodeChat(r, &query)
		if err != nil {
			respond(w, nil, err)
			return
		}
		value, err := activities.Read(r.Context(), r.Context().Value(actorKey{}).(app.Actor), query)
		respond(w, value, err)
	}))
}

func activityQuery(r *http.Request) (domain.LeaderActivityQuery, error) {
	query := domain.LeaderActivityQuery{Limit: 100}
	values, err := r.URL.Query(), error(nil)
	for key, entries := range values {
		if len(entries) != 1 {
			return query, domain.Fail("invalid_input", "duplicate activity query field")
		}
		switch key {
		case "after":
			query.After, err = strconv.ParseInt(entries[0], 10, 64)
		case "limit":
			query.Limit, err = strconv.Atoi(entries[0])
		case "kind":
			query.Kind = entries[0]
		case "id":
			query.ID = entries[0]
		case "generation":
			query.Generation, err = strconv.ParseInt(entries[0], 10, 64)
		default:
			return query, domain.Fail("invalid_input", "unknown activity query field")
		}
		if err != nil {
			return query, domain.Fail("invalid_input", "invalid activity query number")
		}
	}
	return query, domain.ValidateLeaderActivityQuery(query)
}
