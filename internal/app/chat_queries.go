package app

import (
	"context"
	"coordica/internal/domain"
	"coordica/internal/store"
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

func chatItems(tx *store.Tx, kind string, a Actor) ([]domain.ChatItem, error) {
	out := []domain.ChatItem{}
	var binding domain.Credential
	if a.Role == "chat" {
		var e error
		binding, e = chatCredential(tx, a)
		if e != nil {
			return nil, e
		}
		if binding.ChatScope == domain.ChatScopeNode {
			switch kind {
			case "requirements", "runs", "agents":
			default:
				return nil, domain.Fail("forbidden", "center interaction required for this query kind")
			}
		}
	}
	switch kind {
	case "requirements":
		all, e := store.List[domain.Requirement](tx, "requirement")
		if e != nil {
			return nil, e
		}
		for _, v := range all {
			out = append(out, domain.ChatItem{ID: v.ID, Version: v.Version, Kind: kind, Title: redacted(v.Title), Text: redacted(v.Body), Summary: redacted(v.AssistantSummary), Source: v.Source, SourceNodeID: v.SourceNodeID, Truncated: len(v.Body) > 16<<10 || len(v.Title) > 16<<10})
		}
	case "runs":
		runs, e := chatRuns(tx, binding)
		if e != nil {
			return nil, e
		}
		for _, r := range runs {
			r := r
			out = append(out, domain.ChatItem{ID: r.ID, Version: r.Version, Kind: kind, Status: r.State, Tool: r.Tool, Run: &r})
		}
	case "blocks":
		var e error
		out, e = chatBlocks(tx, binding)
		if e != nil {
			return nil, e
		}
	case "tasks":
		all, e := store.List[domain.Task](tx, "task")
		if e != nil {
			return nil, e
		}
		for _, v := range all {
			out = append(out, domain.ChatItem{ID: v.ID, Version: v.Version, Kind: kind, Title: redacted(v.Title), Text: redacted(v.Description), Summary: redacted(v.Acceptance), Status: v.State, GoalVersion: v.GoalVersion, RequirementIDs: safeIdentifiers(v.RequirementIDs), Dependencies: safeIdentifiers(v.Dependencies), AllowedPaths: safeRelativePaths(v.AllowedPaths), Tool: v.Tool, ApprovedVersion: v.ApprovedVersion, AuthorizationVersion: v.AuthorizationVersion, BudgetSeconds: v.BudgetSeconds, UsedSeconds: v.UsedSeconds, ReservedSeconds: v.ReservedSeconds, Truncated: len(v.Title) > 16<<10 || len(v.Description) > 16<<10 || len(v.Acceptance) > 16<<10 || len(v.AllowedPaths) > 128 || len(v.RequirementIDs) > 128 || len(v.Dependencies) > 128})
		}
	case "agents":
		all, e := store.List[domain.Node](tx, "node")
		if e != nil {
			return nil, e
		}
		runs, e := chatRuns(tx, binding)
		if e != nil {
			return nil, e
		}
		for _, v := range all {
			if binding.ChatScope == domain.ChatScopeNode && v.ID != binding.NodeID {
				continue
			}
			nodeRuns := []domain.ChatRun{}
			for _, r := range runs {
				if r.NodeID == v.ID {
					r.TaskTitle = ""
					r.TaskText = ""
					nodeRuns = append(nodeRuns, r)
				}
			}
			sort.Slice(nodeRuns, func(i, j int) bool { return nodeRuns[i].StartedAt.After(nodeRuns[j].StartedAt) })
			truncated := len(nodeRuns) > 20 || len(v.Capabilities) > 64
			if len(nodeRuns) > 20 {
				nodeRuns = nodeRuns[:20]
			}
			status := "registered"
			if v.Revoked {
				status = "revoked"
			} else if v.Draining || v.DisabledNewWork {
				status = "disabled"
			}
			out = append(out, domain.ChatItem{ID: v.ID, Version: v.Version, Kind: kind, Title: redacted(v.Name), Status: status, Capacity: v.Capacity, ObservedAt: &v.LastSeen, Capabilities: safeCapabilities(v.Capabilities), Runs: nodeRuns, Truncated: truncated})
		}
	case "consultations", "proposals":
		all, e := store.List[domain.ConversationRequest](tx, "conversation")
		if e != nil {
			return nil, e
		}
		for _, v := range all {
			if a.Role != "owner" && v.Source.ClientID != a.ID {
				continue
			}
			if kind == "proposals" && v.Kind != "proposal" || kind == "consultations" && v.Kind != "consultation" {
				continue
			}
			v = safeConversation(v)
			d, e := goalDocument(tx)
			if e != nil {
				return nil, e
			}
			v.Stale = v.Stale || v.GoalVersion != d.Version
			out = append(out, domain.ChatItem{ID: v.ID, Version: v.Version, Kind: kind, Text: v.OriginalText, Summary: v.AssistantSummary, Status: v.Status, GoalVersion: v.GoalVersion, Source: &v.Source, Conversation: &v})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
func (s *Service) ChatOverview(ctx context.Context, a Actor) (domain.ChatOverview, error) {
	var out domain.ChatOverview
	e := s.DB.View(ctx, func(tx *store.Tx) error {
		binding, e := chatCredential(tx, a)
		if e != nil {
			return e
		}
		p, e := getProject(tx)
		if e != nil {
			return e
		}
		d, e := goalDocument(tx)
		if e != nil {
			return e
		}
		out = domain.ChatOverview{ChatScope: binding.ChatScope, NodeID: binding.NodeID, ApprovedGoalVersion: d.ApprovedVersion, GoalAuthorizationVersion: d.AuthorizationVersion, DirectoryOutline: redacted(d.DirectoryOutline), AllowedRoots: safeRelativePaths(d.AllowedRoots), RequirementIDs: safeIdentifiers(d.RequirementIDs), APIVersion: domain.APIVersion, QueriedAt: s.Now(), ProjectID: p.ID, Version: p.Version, Name: redacted(p.Name), Paused: p.Paused, GoalVersion: d.Version, GoalStatus: d.Status, Objective: redacted(d.Objective), Architecture: redacted(d.Architecture), NonGoals: redacted(d.NonGoals), Acceptance: redacted(d.Acceptance), UsedSeconds: p.UsedSeconds, ReservedSeconds: p.ReservedSeconds}
		var account domain.ConsultationAccount
		if e = tx.Get("consultation_account", "project", &account); e == nil {
			out.ConsultationAccount = &account
		} else if !errors.Is(e, domain.ErrNotFound) {
			return e
		}
		return nil
	})
	return out, e
}
func (s *Service) ChatList(ctx context.Context, a Actor, in domain.ChatListQuery) (domain.ChatPage, error) {
	var out domain.ChatPage
	if domain.ValidateChatList(in) != nil {
		return out, domain.Fail("invalid_input", "closed kind, bounded cursor and limit 1..20 required")
	}
	e := s.DB.View(ctx, func(tx *store.Tx) error {
		if e := chatCheck(a)(tx); e != nil {
			return e
		}
		p, e := getProject(tx)
		if e != nil {
			return e
		}
		all, e := chatItems(tx, in.Kind, a)
		if e != nil {
			return e
		}
		out = domain.ChatPage{QueriedAt: s.Now(), Version: p.Version, Items: []domain.ChatItem{}}
		bytes := 0
		for _, item := range all {
			if item.ID <= in.Cursor {
				continue
			}
			raw, err := json.Marshal(item)
			if err != nil {
				return err
			}
			if len(out.Items) == in.Limit || bytes+len(raw) > 1<<20 {
				if len(out.Items) == 0 {
					return domain.Fail("invalid_input", "record exceeds bounded response")
				}
				out.NextCursor = out.Items[len(out.Items)-1].ID
				break
			}
			out.Items = append(out.Items, item)
			bytes += len(raw)
		}
		return nil
	})
	return out, e
}
func (s *Service) ChatItem(ctx context.Context, a Actor, in domain.ChatItemQuery) (domain.ChatDetail, error) {
	var out domain.ChatDetail
	if domain.ValidateChatItem(in) != nil {
		return out, domain.Fail("invalid_input", "valid kind and identifier required")
	}
	e := s.DB.View(ctx, func(tx *store.Tx) error {
		if e := chatCheck(a)(tx); e != nil {
			return e
		}
		all, e := chatItems(tx, in.Kind, a)
		if e != nil {
			return e
		}
		for _, item := range all {
			if item.ID == in.ID {
				out = domain.ChatDetail{QueriedAt: s.Now(), Version: item.Version, Item: item}
				return nil
			}
		}
		return domain.ErrNotFound
	})
	return out, e
}

func safeRelativePaths(paths []string) []string {
	out := []string{}
	for _, p := range paths {
		if len(out) == 128 {
			break
		}
		if len(p) <= 512 && !strings.HasPrefix(p, "/") && !strings.Contains(p, "\\") && !strings.Contains(p, ":") && !strings.Contains(p, "..") && !strings.ContainsAny(p, "\x00\r\n") {
			out = append(out, p)
		}
	}
	return out
}

func safeIdentifiers(ids []string) []string {
	out := []string{}
	for _, id := range ids {
		if len(out) == 128 {
			break
		}
		if chatID.MatchString(id) {
			out = append(out, id)
		}
	}
	return out
}
func safeCapabilities(caps []domain.Capability) []domain.ChatAgentCapability {
	out := []domain.ChatAgentCapability{}
	for _, c := range caps {
		if len(out) == 64 {
			break
		}
		out = append(out, domain.ChatAgentCapability{Structured: c.Structured, Resume: c.Resume, Cancel: c.Cancel, Tool: redacted(c.Tool), Version: redacted(c.Version), Protocol: redacted(c.Protocol), Available: c.Available, ReadOnly: c.ReadOnly, Review: c.Review})
	}
	return out
}
