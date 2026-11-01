package app

import (
	"fmt"

	"coordica/internal/domain"
)

func (r workReader) leaderFact(ref domain.WorkReference) (domain.WorkFact, error) {
	switch ref.Kind {
	case "consultation":
		return r.consultationFact(ref)
	case "initialization":
		return r.initializationFact(ref)
	case "goal":
		return r.goalFact(ref)
	}
	return domain.WorkFact{}, domain.ErrNotFound
}

func (r workReader) consultationFact(ref domain.WorkReference) (domain.WorkFact, error) {
	var q domain.ConversationRequest
	f := domain.WorkFact{Reference: domain.WorkReference{Kind: ref.Kind, ID: ref.ID}}
	if err := r.tx.Get("conversation", ref.ID, &q); err != nil {
		return f, err
	}
	if r.actor.Role != "owner" && q.Source.ClientID != r.actor.ID {
		return f, domain.ErrNotFound
	}
	d, err := goalDocument(r.tx)
	if err != nil {
		return f, err
	}
	f.Reference.Version = q.Version
	f.State, f.Stale, f.NextAction = q.Status, q.Stale || q.GoalVersion != d.Version, "view_consultation"
	text := q.OriginalText
	if q.Reply != "" {
		text = q.Reply
	}
	workDescription(&f, "Leader 咨询", text)
	return r.seal(f, struct {
		Goal        int64
		Run, Digest string
	}{d.Version, q.RunID, q.ResultSHA256})
}

func (r workReader) initializationFact(ref domain.WorkReference) (domain.WorkFact, error) {
	if ref.ID != r.project.ID {
		return domain.WorkFact{}, domain.ErrNotFound
	}
	v, err := initialization(r.tx)
	if err != nil {
		return domain.WorkFact{}, err
	}
	if v.Version == 0 {
		return domain.WorkFact{}, domain.ErrNotFound
	}
	if r.actor.Role != "owner" {
		v = safeInitialization(v)
	}
	f := domain.WorkFact{Reference: domain.WorkReference{Kind: ref.Kind, ID: ref.ID, Version: v.Version,
		Generation: v.OperationGeneration}, Title: "项目初始化", State: v.Status, NextAction: "view_initialization"}
	if v.Result != nil {
		for _, question := range v.Result.Questions {
			if len(f.Questions) == 16 {
				f.Truncated = true
				break
			}
			text, clipped := workText(question.Text)
			f.Truncated = f.Truncated || clipped
			state := "recorded"
			if v.Status == "interview" || v.Status == "awaiting_owner" {
				state = "awaiting_answer"
			}
			if v.Status == "analyzing" {
				state = "processing"
			}
			if v.Status == "initialized" {
				state = "closed"
			}
			f.Questions = append(f.Questions, domain.WorkQuestion{ID: question.ID, Text: text, State: state, NextAction: "view_initialization"})
		}
	}
	return r.seal(f, struct {
		Operation string
		Version   int64
		Digest    string
		Feedback  []domain.InitializationFeedback
	}{v.OperationID, v.OperationVersion, v.Digest, v.Feedback})
}

func (r workReader) goalFact(ref domain.WorkReference) (domain.WorkFact, error) {
	if ref.ID != r.project.ID {
		return domain.WorkFact{}, domain.ErrNotFound
	}
	d, err := goalDocument(r.tx)
	if err != nil {
		return domain.WorkFact{}, err
	}
	if d.Version == 0 {
		return domain.WorkFact{}, domain.ErrNotFound
	}
	f := domain.WorkFact{Reference: domain.WorkReference{Kind: ref.Kind, ID: ref.ID, Version: d.Version,
		AuthorizationVersion: d.AuthorizationVersion}, State: d.Status, NextAction: "view_goal"}
	workDescription(&f, "阶段目标", d.Objective)
	// Existing limited chat can read shared Goal facts, not arbitrary private
	// model activity. Owner's question view retains its original authority.
	if r.actor.Role == "owner" && d.LeaderResult != nil {
		for index, text := range d.LeaderResult.Clarifications {
			if len(f.Questions) == 16 {
				f.Truncated = true
				break
			}
			text, clipped := workText(text)
			f.Truncated = f.Truncated || clipped
			f.Questions = append(f.Questions, domain.WorkQuestion{ID: fmt.Sprintf("%s:%d", d.LeaderRunID, index), Text: text,
				State: "recorded_clarification", NextAction: "view_goal"})
		}
	}
	if r.actor.Role != "owner" {
		return r.seal(f, nil)
	}
	return r.seal(f, struct {
		Run    string
		Result *domain.LeaderResult
	}{d.LeaderRunID, d.LeaderResult})
}
