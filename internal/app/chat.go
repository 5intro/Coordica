package app

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func boundedText(v string, max int, required bool) bool { return domain.ChatText(v, max, required) }

var chatID = domain.ChatIdentifier

func sourceInput(a Actor, source domain.ChatSource) (domain.ChatSource, error) {
	if source.ClientID != "" && source.ClientID != a.ID {
		return source, domain.Fail("forbidden", "source client must match authenticated client")
	}
	source.ClientID = a.ID
	if !chatID.MatchString(source.SessionID) || !chatID.MatchString(source.MessageID) {
		return source, domain.Fail("invalid_input", "bounded session/message identifiers required")
	}
	return source, nil
}
func (s *Service) createRequirement(tx *store.Tx, a Actor, r domain.Requirement) (any, error) {
	r.RelatedIDs = nil // relationships are written symmetrically by LinkRequirements
	if strings.TrimSpace(r.Title) == "" || strings.TrimSpace(r.Body) == "" {
		return nil, domain.Fail("invalid_input", "requirement title and text required")
	}
	if e := domain.ValidateRequirementProposal(domain.RequirementProposal{Title: r.Title, Body: r.Body}); e != nil {
		return nil, e
	}
	if _, e := getProject(tx); e != nil {
		return nil, e
	}
	r.ID = ID("req_")
	r.Version = 1
	r.CreatedAt = s.Now()
	r.ContentRevision, r.State, r.UpdatedAt = 1, "active", r.CreatedAt
	if e := tx.Put("requirement", r.ID, r); e != nil {
		return nil, e
	}
	if e := s.saveRequirementRevision(tx, a, r, ""); e != nil {
		return nil, e
	}
	if e := s.queueRequirementAddition(tx, r); e != nil {
		return nil, e
	}
	if a.Role == "chat" {
		r.Title = redacted(r.Title)
		r.Body = redacted(r.Body)
		r.OriginalText = redacted(r.OriginalText)
		r.AssistantSummary = redacted(r.AssistantSummary)
	}
	kind := "requirement.created"
	if a.Role == "node" {
		kind = "requirement.submitted"
	}
	detail := ""
	if a.Role != "chat" {
		detail = r.Title
	}
	return r, s.audit(tx, a, kind, r.ID, detail)
}
func (s *Service) ChatSubmitRequirement(ctx context.Context, a Actor, key string, in domain.ChatRequirementInput) (json.RawMessage, error) {
	source, e := sourceInput(a, in.Source)
	if e != nil {
		return nil, e
	}
	if domain.ValidateChatRequirement(in) != nil {
		return nil, domain.Fail("invalid_input", "bounded requirement title, original text and source required")
	}
	in.Source = source
	return s.mutateWithCheck(ctx, a, key, "chat.requirement.submit", in, chatCheck(a), func(tx *store.Tx) (any, error) {
		binding, e := chatCredential(tx, a)
		if e != nil {
			return nil, e
		}
		if response, ok, e := chatSourceReplay(tx, "requirement", source, in); e != nil || ok {
			return response, e
		}
		result, e := s.createRequirement(tx, a, domain.Requirement{SourceNodeID: binding.NodeID, Title: in.Title, Body: in.OriginalText, OriginalText: in.OriginalText, AssistantSummary: in.AssistantSummary, Source: &source})
		if e != nil {
			return nil, e
		}
		return result, rememberChatSource(tx, "requirement", source, in, result)
	})
}
func goalDocument(tx *store.Tx) (domain.PlanningDocument, error) {
	var d domain.PlanningDocument
	e := tx.Get("planning_document", "current", &d)
	if errors.Is(e, domain.ErrNotFound) {
		e = nil
	}
	return d, e
}
func (s *Service) ConsultLeader(ctx context.Context, a Actor, key, kind string, in domain.ConversationInput) (json.RawMessage, error) {
	if kind != "consultation" && kind != "proposal" {
		return nil, domain.Fail("invalid_input", "invalid conversation kind")
	}
	source, e := sourceInput(a, in.Source)
	if e != nil {
		return nil, e
	}
	if domain.ValidateConversation(in) != nil {
		return nil, domain.Fail("invalid_input", "bounded conversation and goal version required")
	}
	in.Source = source
	return s.mutateWithCheck(ctx, a, key, "chat."+kind, in, func(tx *store.Tx) error {
		if e := conversationCheck(a)(tx); e != nil {
			return e
		}
		if in.ReplyTo != "" {
			var prev domain.ConversationRequest
			if tx.Get("conversation", in.ReplyTo, &prev) != nil || prev.Source.ClientID != source.ClientID || prev.Source.SessionID != source.SessionID || prev.GoalVersion != in.ExpectedGoalVersion {
				return domain.ErrNotFound
			}
		}
		return nil
	}, func(tx *store.Tx) (any, error) {
		if response, ok, e := chatSourceReplay(tx, kind, source, in); e != nil || ok {
			return response, e
		}
		p, e := getProject(tx)
		if e != nil {
			return nil, e
		}
		if p.Paused {
			return nil, domain.Fail("conflict", "project paused")
		}
		d, e := goalDocument(tx)
		if e != nil {
			return nil, e
		}
		if e = version(d.Version, in.ExpectedGoalVersion); e != nil {
			return nil, e
		}

		settings, e := automation(tx)
		if e != nil {
			return nil, e
		}
		var account domain.ConsultationAccount
		e = tx.Get("consultation_account", "project", &account)
		if errors.Is(e, domain.ErrNotFound) {
			account = domain.ConsultationAccount{ID: "project"}
			e = tx.Put("consultation_account", account.ID, account)
		}
		if e != nil {
			return nil, e
		}

		runs, e := store.List[domain.PlanningRun](tx, "planning_run")
		if e != nil {
			return nil, e
		}
		for _, r := range runs {
			if r.State == "queued" || r.State == "running" || r.State == "pending_result" || r.State == "cancel_requested" || r.State == "unknown_live" {
				return nil, domain.Fail("conflict", "Leader slot requires previous run to settle and stop")
			}
		}
		q := domain.ConversationRequest{ID: ID("consult_"), Version: 1, Kind: kind, OriginalText: in.OriginalText, AssistantSummary: in.AssistantSummary, Source: source, ReplyTo: in.ReplyTo, GoalVersion: d.Version, CreatedAt: s.Now(), Status: "queued"}
		baseline, e := initialization(tx)
		if e != nil {
			return nil, e
		}
		r := domain.PlanningRun{BaselineVersion: baseline.ApprovedVersion, BaselineDigest: baseline.ApprovedDigest, ID: ID("plan_"), Version: 1, Purpose: domain.ConsultationPurpose, ConversationID: q.ID, AccountingTarget: account.ID, DocumentVersion: d.Version, AuthorizationVersion: d.AuthorizationVersion, Generation: 1, State: "queued", MaxRetries: settings.PlanningMaxRetries, AutomationVersion: settings.Version}
		q.RunID = r.ID
		if e = tx.Put("planning_run", r.ID, r); e != nil {
			return nil, e
		}
		if e = tx.Put("conversation", q.ID, q); e != nil {
			return nil, e
		}
		if e = s.audit(tx, a, "chat."+kind+".submitted", q.ID, ""); e != nil {
			return nil, e
		}
		response := safeConversation(q)
		return response, rememberChatSource(tx, kind, source, in, response)
	})
}
func (s *Service) cancelConversationRun(tx *store.Tx, r *domain.PlanningRun) error {
	if r.State != "queued" && r.State != "running" && r.State != "pending_result" {
		return nil
	}
	r.State = "cancel_requested"
	r.Version++
	if r.GrantSeconds == 0 || r.Settled {
		r.State = "stopped"
		r.TreeStopped = true
		r.Settled = true
	}
	if e := tx.Put("planning_run", r.ID, r); e != nil {
		return e
	}
	return syncConversation(tx, *r)
}
func syncConversation(tx *store.Tx, r domain.PlanningRun) error {
	if r.Purpose != domain.ConsultationPurpose {
		return nil
	}
	var q domain.ConversationRequest
	if e := tx.Get("conversation", r.ConversationID, &q); e != nil {
		return e
	}
	if q.RunID != r.ID {
		return nil
	}
	q.Status = r.State
	q.Version++
	return tx.Put("conversation", q.ID, q)
}
func (s *Service) CancelConversation(ctx context.Context, a Actor, key string, in domain.ChatCancelRequest) (json.RawMessage, error) {
	if !chatID.MatchString(in.ID) || in.ExpectedVersion < 1 {
		return nil, domain.Fail("invalid_input", "bounded request ID and positive version required")
	}
	return s.mutateWithCheck(ctx, a, key, "chat.cancel", in, conversationCheck(a), func(tx *store.Tx) (any, error) {
		var q domain.ConversationRequest
		if e := tx.Get("conversation", in.ID, &q); e != nil {
			return nil, e
		}
		if a.Role != "owner" && q.Source.ClientID != a.ID {
			return nil, domain.ErrNotFound
		}
		if e := version(q.Version, in.ExpectedVersion); e != nil {
			return nil, e
		}
		var r domain.PlanningRun
		if e := tx.Get("planning_run", q.RunID, &r); e != nil {
			return nil, e
		}
		if e := s.cancelConversationRun(tx, &r); e != nil {
			return nil, e
		}
		if e := tx.Get("conversation", q.ID, &q); e != nil {
			return nil, e
		}
		return safeConversation(q), s.audit(tx, a, "chat.cancelled", q.ID, "")
	})
}

// Stop confirmation remains an Owner-only operational use case, including when
// no goal exists. It cannot grant advisory content development authority.
func (s *Service) ConfirmConsultationStopped(ctx context.Context, a Actor, key, id string, in domain.StopConfirmation) (json.RawMessage, error) {
	if e := owner(a); e != nil {
		return nil, e
	}
	return s.mutate(ctx, a, key, "chat.confirm_stopped."+id, in, func(tx *store.Tx) (any, error) {
		var r domain.PlanningRun
		if e := tx.Get("planning_run", id, &r); e != nil {
			return nil, e
		}
		if r.Purpose != domain.ConsultationPurpose || r.State != "unknown_live" || !boundedText(in.Evidence, 4096, true) {
			return nil, domain.Fail("conflict", "unknown advisory run and stop evidence required")
		}
		if e := version(r.Version, in.ExpectedVersion); e != nil {
			return nil, e
		}
		r.TreeStopped = true
		r.State = "stopped"
		r.Version++
		if e := tx.Put("planning_run", id, r); e != nil {
			return nil, e
		}
		if e := syncConversation(tx, r); e != nil {
			return nil, e
		}
		return r, s.audit(tx, a, "chat.stop_confirmed", id, in.Evidence)
	})
}

// Redaction is applied to text as well as selecting safe DTO fields. Structured
// executable and path fields are never included. Natural language is untrusted.
var chatSecrets = regexp.MustCompile(`(?i)(?:own_|node_|chat_|sk-)[a-z0-9_-]+|(?:bearer\s+)[a-z0-9._-]+|(?:token|password|api[_-]?key|secret)\s*[:=]\s*[^\s,;]+`)
var chatPaths = regexp.MustCompile(`(?m)(?:^|[\s"'=:(])(?:[A-Za-z]:[\\/]|\\\\[^\s\\]+\\|/(?:Users|home|root|private|tmp|var|etc|opt|srv|data|workspace|workspaces|nix|Volumes|Applications|Library|System|mnt|media|usr)(?:/|\b))[^\s"'<>]*`)
var chatCommands = regexp.MustCompile(`(?im)^(?:\$\s|command\s*[:=]|argv\s*[:=]|executable\s*[:=]).*$`)

func redacted(v string) string {
	v = chatSecrets.ReplaceAllString(v, "[redacted]")
	v = chatPaths.ReplaceAllString(v, "[local path redacted]")
	v = chatCommands.ReplaceAllString(v, "[command redacted]")
	if len(v) > 16<<10 {
		v = v[:16<<10]
		for !utf8.ValidString(v) {
			v = v[:len(v)-1]
		}
	}
	return v
}
func safeConversation(q domain.ConversationRequest) domain.ConversationRequest {
	q.OriginalText = redacted(q.OriginalText)
	q.AssistantSummary = redacted(q.AssistantSummary)
	q.Reply = redacted(q.Reply)
	return q
}
