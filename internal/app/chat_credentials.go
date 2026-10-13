package app

import (
	"context"
	"coordica/internal/domain"
	"coordica/internal/store"
	"encoding/json"
)

// Resolve persisted scope in every read/write transaction; Actor fields and
// source identifiers are never evidence of host authority.
func chatCredential(tx *store.Tx, a Actor) (domain.Credential, error) {
	if a.Role != "chat" {
		return domain.Credential{}, domain.Fail("forbidden", "chat credential required")
	}
	all, e := store.List[domain.Credential](tx, "credential")
	if e != nil {
		return domain.Credential{}, e
	}
	for _, c := range all {
		if c.ID != a.ID || c.Role != "chat" {
			continue
		}
		if c.Revoked {
			break
		}
		switch c.ChatScope {
		case domain.ChatScopeCenter:
			if c.NodeID != "" {
				break
			}
			return c, nil
		case domain.ChatScopeNode:
			if _, e := nodeConsoleActor(tx, Actor{ID: c.NodeID, Role: "node", NodeID: c.NodeID}); e != nil {
				return domain.Credential{}, e
			}
			return c, nil
		}
		break
	}
	return domain.Credential{}, domain.Fail("unauthorized", "chat credential revoked or unscoped; explicit renewal required")
}
func chatCheck(a Actor) func(*store.Tx) error {
	return func(tx *store.Tx) error { _, e := chatCredential(tx, a); return e }
}
func conversationCheck(a Actor) func(*store.Tx) error {
	return func(tx *store.Tx) error {
		if a.Role == "owner" {
			return nil
		}
		c, e := chatCredential(tx, a)
		if e != nil {
			return e
		}
		if c.ChatScope != domain.ChatScopeCenter {
			return domain.Fail("forbidden", "only center-host interaction may contact Leader")
		}
		return nil
	}
}
func (s *Service) ChatIdentity(ctx context.Context, a Actor) (domain.ChatCredentialInfo, error) {
	var out domain.ChatCredentialInfo
	e := s.DB.View(ctx, func(tx *store.Tx) error {
		c, e := chatCredential(tx, a)
		if e != nil {
			return e
		}
		out = credentialInfo(c)
		return nil
	})
	return out, e
}
func (s *Service) CreateChatCredential(ctx context.Context, a Actor, key string, in domain.ChatCredentialRequest) (json.RawMessage, error) {
	if e := owner(a); e != nil {
		return nil, e
	}
	return s.grantChatCredential(ctx, a, key, in, domain.ChatScopeCenter, "")
}
func (s *Service) CreateNodeChatCredential(ctx context.Context, a Actor, key string, in domain.ChatCredentialRequest) (json.RawMessage, error) {
	if a.Role != "node" {
		return nil, domain.Fail("forbidden", "paired node credential required")
	}
	return s.grantChatCredential(ctx, a, key, in, domain.ChatScopeNode, "")
}

// Issuance authority comes from a live persisted node credential, including on
// idempotent replay. Actor metadata alone is never evidence of that authority.
func nodeChatIssuer(tx *store.Tx, a Actor) (string, error) {
	if a.Role != "node" {
		return "", domain.Fail("forbidden", "paired node credential required")
	}
	all, e := store.List[domain.Credential](tx, "credential")
	if e != nil {
		return "", e
	}
	for _, c := range all {
		if c.ID != a.ID || c.Role != "node" || c.Revoked {
			continue
		}
		if c.NodeID == "" || a.NodeID != "" && a.NodeID != c.NodeID {
			return "", domain.Fail("forbidden", "node credential binding mismatch")
		}
		var n domain.Node
		if tx.Get("node", c.NodeID, &n) != nil || n.ID != c.NodeID || n.Revoked {
			return "", domain.Fail("forbidden", "active registered node required")
		}
		return c.NodeID, nil
	}
	return "", domain.Fail("forbidden", "active persisted node credential required")
}
func (s *Service) grantChatCredential(ctx context.Context, a Actor, key string, in domain.ChatCredentialRequest, scope, nodeID string) (json.RawMessage, error) {
	if !boundedText(in.Name, 128, true) {
		return nil, domain.Fail("invalid_input", "bounded client name required")
	}
	action := "chat.credential.create"
	check := func(tx *store.Tx) error {
		if scope == domain.ChatScopeNode {
			var e error
			if nodeID, e = nodeChatIssuer(tx, a); e != nil {
				return e
			}
		}
		// Replay can never resurrect a revoked or rebound grant.
		b, _ := json.Marshal(in)
		raw, ok, e := tx.Replay(a.ID, key, Hash(action+"\x00"+string(b)))
		if e != nil || !ok {
			return e
		}
		var g domain.ChatCredentialGrant
		if json.Unmarshal(raw, &g) != nil {
			return domain.Fail("unauthorized", "invalid cached grant")
		}
		var c domain.Credential
		if tx.Get("credential", Hash(g.Token), &c) != nil || c.Revoked || c.Role != "chat" || c.ID != g.Credential.ID || c.ChatScope != scope || c.NodeID != nodeID {
			return domain.Fail("unauthorized", "cached chat grant revoked or rebound; explicit renewal required")
		}
		return nil
	}
	return s.mutateWithCheck(ctx, a, key, action, in, check, func(tx *store.Tx) (any, error) {
		token := ID("chat_")
		c := domain.Credential{ID: ID("client_"), Hash: Hash(token), Role: "chat", ChatScope: scope, NodeID: nodeID, Name: in.Name, CreatedAt: s.Now()}
		if e := tx.Put("credential", c.Hash, c); e != nil {
			return nil, e
		}
		return domain.ChatCredentialGrant{Credential: credentialInfo(c), Token: token}, s.audit(tx, a, "chat.credential.created", c.ID, "")
	})
}
func credentialInfo(c domain.Credential) domain.ChatCredentialInfo {
	return domain.ChatCredentialInfo{ChatScope: c.ChatScope, NodeID: c.NodeID, ID: c.ID, Name: c.Name, Revoked: c.Revoked, CreatedAt: c.CreatedAt}
}
func (s *Service) RevokeChatCredential(ctx context.Context, a Actor, key, id string) (json.RawMessage, error) {
	if a.Role != "owner" && a.Role != "node" {
		return nil, domain.Fail("forbidden", "Owner or issuing node required")
	}
	check := func(tx *store.Tx) error {
		if a.Role == "owner" {
			return nil
		}
		nodeID, e := nodeChatIssuer(tx, a)
		if e != nil {
			return e
		}
		all, e := store.List[domain.Credential](tx, "credential")
		if e != nil {
			return e
		}
		for _, c := range all {
			if c.ID == id && c.Role == "chat" && c.ChatScope == domain.ChatScopeNode && c.NodeID == nodeID {
				return nil
			}
		}
		return domain.ErrNotFound
	}
	return s.mutateWithCheck(ctx, a, key, "chat.credential.revoke."+id, id, check, func(tx *store.Tx) (any, error) {
		all, e := store.List[domain.Credential](tx, "credential")
		if e != nil {
			return nil, e
		}
		for _, c := range all {
			if c.ID == id && c.Role == "chat" {
				c.Revoked = true
				if e = tx.Put("credential", c.Hash, c); e != nil {
					return nil, e
				}
				runs, e := store.List[domain.PlanningRun](tx, "planning_run")
				if e != nil {
					return nil, e
				}
				for _, r := range runs {
					if r.Purpose != domain.ConsultationPurpose {
						continue
					}
					var q domain.ConversationRequest
					if e = tx.Get("conversation", r.ConversationID, &q); e != nil {
						return nil, e
					}
					if q.Source.ClientID == id {
						if e = s.cancelConversationRun(tx, &r); e != nil {
							return nil, e
						}
					}
				}
				return credentialInfo(c), s.audit(tx, a, "chat.credential.revoked", id, "")
			}
		}
		return nil, domain.ErrNotFound
	})
}
