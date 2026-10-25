package app

import (
	"context"
	"coordica/internal/domain"
	"coordica/internal/store"
	"encoding/json"
	"slices"
	"strings"
)

// Clarification replies bind to the exact accepted Leader result as well as
// its goal version. A new result for the same document invalidates old forms.
func validatePlanningAnswer(tx *store.Tx, document domain.PlanningDocument, request domain.PlanningMessageRequest) error {
	if request.ReplyToRunID == "" && request.Question == "" {
		return nil
	}
	if request.ReplyToRunID == "" || request.ReplyToRunID != document.LeaderRunID || document.LeaderResult == nil {
		return domain.Fail("version_conflict", "Leader 问题已更新，请核对当前问题")
	}
	var source domain.PlanningRun
	if err := tx.Get("planning_run", request.ReplyToRunID, &source); err != nil {
		return err
	}
	if source.ScopeRequestID != "" || source.RequirementChangeID != "" || source.Purpose == domain.ConsultationPurpose {
		return domain.Fail("version_conflict", "专项评估的问题必须在原评估流程中处理")
	}
	result := document.LeaderResult
	if !slices.Contains(result.Clarifications, request.Question) && !slices.Contains(result.MissingItems, request.Question) {
		return domain.Fail("version_conflict", "问题不属于当前 Leader 结果")
	}
	if strings.TrimSpace(request.Text) == "" || len(request.Question) > 64<<10 {
		return domain.Fail("invalid_input", "回答不能为空")
	}
	return nil
}

func planningAnswerText(request domain.PlanningMessageRequest) string {
	if request.ReplyToRunID == "" {
		return request.Text
	}
	return "Leader 问题：\n" + request.Question + "\n\nOwner 原始回答：\n" + request.Text
}

func (s *Service) SchedulePlanning(ctx context.Context, a Actor, key string,
	req domain.PlanningMessageRequest,
) (json.RawMessage, error) {
	if e := owner(a); e != nil {
		return nil, e
	}
	if len(req.Text) > 64<<10 {
		return nil, domain.Fail("invalid_input", "message too large")
	}
	return s.mutateWithCheck(ctx, a, key, "planning.evaluate", req,
		func(tx *store.Tx) error { return baselineGate(tx, nil, false) }, func(tx *store.Tx) (any, error) {
			var d domain.PlanningDocument
			if e := tx.Get("planning_document", "current", &d); e != nil {
				return nil, e
			}
			if e := version(d.Version, req.ExpectedVersion); e != nil {
				return nil, e
			}
			if e := validatePlanningAnswer(tx, d, req); e != nil {
				return nil, e
			}
			if pending, e := pendingGoalRequirementChange(tx, d); e != nil {
				return nil, e
			} else if pending {
				return nil, domain.Fail("conflict", "先处理当前目标中的需求变更")
			}
			settings, e := automation(tx)
			if e != nil {
				return nil, e
			}
			runs, e := store.List[domain.PlanningRun](tx, "planning_run")
			if e != nil {
				return nil, e
			}
			for _, r := range runs {
				if r.State == "queued" || r.State == "running" || r.State == "cancel_requested" || r.State == "unknown_live" {
					return nil, domain.Fail("conflict", "planning run must stop before another evaluation")
				}
			}
			var message domain.LeaderMessage
			if strings.TrimSpace(req.Text) != "" {
				message = domain.LeaderMessage{ID: ID("msg_"), DocumentVersion: d.Version, Role: "owner", Text: planningAnswerText(req), CreatedAt: s.Now()}
				if e := tx.Put("leader_message", message.ID, message); e != nil {
					return nil, e
				}
			}
			baseline, e := initialization(tx)
			if e != nil {
				return nil, e
			}
			r := domain.PlanningRun{
				BaselineVersion: baseline.ApprovedVersion, BaselineDigest: baseline.ApprovedDigest,
				ID: ID("plan_"), Version: 1, DocumentVersion: d.Version, AuthorizationVersion: d.AuthorizationVersion,
				AutomationVersion: settings.Version, Generation: 1, State: "queued", Leader: leaderChoice(settings), MaxRetries: settings.PlanningMaxRetries,
			}
			if e = tx.Put("planning_run", r.ID, r); e != nil {
				return nil, e
			}
			var receipt any = r
			if req.ReplyToRunID != "" {
				receipt = domain.PlanningAnswerReceipt{ReplyToRunID: req.ReplyToRunID, Run: r, Message: message}
			}
			return receipt, s.audit(tx, a, "planning.scheduled", r.ID, "")
		})
}

func recordLeaderAnswer(tx *store.Tx, s *Service, r *domain.PlanningRun, d *domain.PlanningDocument) error {
	r.State = "succeeded"
	if r.ScopeRequestID != "" {
		// Scope assessments retain their exact request and decision flow;
		// they must never publish ordinary goal questions or answer bindings.
		return nil
	}
	d.LeaderRunID = r.ID
	d.LeaderResult = r.PendingResult
	d.LeaderSession = r.SessionID
	d.MissingItems = append(append(documentMissing(*d), r.PendingResult.MissingItems...), r.PendingResult.Clarifications...)
	m := domain.LeaderMessage{ID: ID("msg_"), DocumentVersion: d.Version, Role: "leader", Text: r.PendingResult.Reply, CreatedAt: s.Now()}
	if e := tx.Put("leader_message", m.ID, m); e != nil {
		return e
	}
	return nil
}

func resetLeaderAnswer(d *domain.PlanningDocument) {
	d.LeaderRunID = ""
	d.LeaderResult = nil
	d.LeaderSession = ""
}
