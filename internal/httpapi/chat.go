package httpapi

import (
	"io"
	"net/http"
	"strings"

	"coordica/internal/app"
	"coordica/internal/domain"
	"coordica/internal/jsoninput"
)

// Explicit closed routes are shared by the loopback and HTTPS node listener.
// No owner operation is mounted in this namespace or on the remote listener.
func chatRoute(r *http.Request) bool {
	if r.Method == "GET" {
		return r.URL.Path == "/api/v1/chat/overview" || r.URL.Path == "/api/v1/chat/identity"
	}
	if r.Method != "POST" {
		return false
	}
	switch r.URL.Path {
	case "/api/v1/chat/items", "/api/v1/chat/item", "/api/v1/chat/requirements", "/api/v1/chat/consultations", "/api/v1/chat/proposals", "/api/v1/chat/consultation", "/api/v1/chat/cancel":
		return true
	}
	return false
}
func decodeChat(r *http.Request, out any) error {
	defer r.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(r.Body, (32<<10)+1))
	if e != nil {
		return domain.Fail("invalid_input", "invalid chat input")
	}
	if _, ok := out.(*domain.ConversationInput); ok {
		if e = jsoninput.Required(raw, "expected_goal_version", "original_text", "source"); e != nil {
			return domain.Fail("invalid_input", "required chat fields missing")
		}
	}
	if e = jsoninput.Decode(raw, out, 32<<10); e != nil {
		return domain.Fail("invalid_input", "invalid chat JSON object")
	}
	return nil
}
func (s *Server) chatAPI(w http.ResponseWriter, r *http.Request) {
	a := r.Context().Value(actorKey{}).(app.Actor)
	ctx := r.Context()
	key := r.Header.Get("Idempotency-Key")
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/chat/")
	var v any
	var e error
	switch path {
	case "identity":
		v, e = s.Service.ChatIdentity(ctx, a)
	case "overview":
		v, e = s.Service.ChatOverview(ctx, a)
	case "items":
		var in domain.ChatListQuery
		if e = decodeChat(r, &in); e == nil {
			v, e = s.Service.ChatList(ctx, a, in)
		}
	case "item":
		var in domain.ChatItemQuery
		if e = decodeChat(r, &in); e == nil {
			v, e = s.Service.ChatItem(ctx, a, in)
		}
	case "consultation":
		var in domain.ChatRequestQuery
		if e = decodeChat(r, &in); e == nil {
			v, e = s.Service.ChatItem(ctx, a, domain.ChatItemQuery{Kind: "consultations", ID: in.ID})
			if e == domain.ErrNotFound {
				v, e = s.Service.ChatItem(ctx, a, domain.ChatItemQuery{Kind: "proposals", ID: in.ID})
			}
		}
	case "requirements":
		var in domain.ChatRequirementInput
		if e = decodeChat(r, &in); e == nil {
			v, e = s.Service.ChatSubmitRequirement(ctx, a, key, in)
		}
	case "consultations", "proposals":
		var in domain.ConversationInput
		if e = decodeChat(r, &in); e == nil {
			kind := "consultation"
			if path == "proposals" {
				kind = "proposal"
			}
			v, e = s.Service.ConsultLeader(ctx, a, key, kind, in)
		}
	case "cancel":
		var in domain.ChatCancelRequest
		if e = decodeChat(r, &in); e == nil {
			v, e = s.Service.CancelConversation(ctx, a, key, in)
		}
	default:
		e = domain.ErrNotFound
	}
	respond(w, v, e)
}
