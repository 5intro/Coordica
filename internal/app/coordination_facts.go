package app

import (
	"coordica/internal/domain"
)

func (r workReader) fact(ref domain.WorkReference) (domain.WorkFact, error) {
	if r.nodeBound() && ref.Kind != "project" && ref.Kind != "requirement" && ref.Kind != "task" && ref.Kind != "run" {
		return domain.WorkFact{}, domain.ErrNotFound
	}
	f := domain.WorkFact{Reference: domain.WorkReference{Kind: ref.Kind, ID: ref.ID}, Questions: []domain.WorkQuestion{}}
	switch ref.Kind {
	case "project":
		return r.projectFact(ref)
	case "requirement":
		var v domain.Requirement
		if err := r.tx.Get("requirement", ref.ID, &v); err != nil {
			return f, err
		}
		f.Reference.Version, f.Reference.Revision = v.Version, v.ContentRevision
		f.State, f.NextAction = v.State, "view_requirement"
		workDescription(&f, v.Title, v.Body)
		return r.seal(f, v.ContentRevision)
	case "task":
		var t domain.Task
		if err := r.tx.Get("task", ref.ID, &t); err != nil {
			return f, err
		}
		if !r.canReadTask(t) {
			return f, domain.ErrNotFound
		}
		f.Reference.Version, f.Reference.AuthorizationVersion, f.Reference.Generation = t.Version, t.AuthorizationVersion, t.Generation
		f.State, f.NextAction = t.State, "view_task"
		workDescription(&f, t.Title, t.Description)
		if t.HoldReason != "" {
			f.State = "blocked"
			f.NextAction = "view_task_block"
		}
		goal, err := goalDocument(r.tx)
		if err != nil {
			return f, err
		}
		baseline, err := initialization(r.tx)
		if err != nil {
			return f, err
		}
		f.Stale = t.GoalVersion > 0 && (t.GoalVersion != goal.Version || t.GoalAuthorizationVersion != goal.AuthorizationVersion) ||
			t.BaselineVersion > 0 && t.BaselineDigest != baseline.ApprovedDigest
		return r.seal(f, struct {
			Goal, GoalAuthority, Baseline int64
			Digest, Run, Candidate, Hold  string
			CurrentGoal, CurrentAuthority int64
			CurrentBaseline               string
		}{
			t.GoalVersion, t.GoalAuthorizationVersion, t.BaselineVersion, t.BaselineDigest, t.CurrentRunID, t.CurrentCandidateID, t.HoldReason,
			goal.Version, goal.AuthorizationVersion, baseline.ApprovedDigest})
	case "run":
		return r.runFact(ref)
	case "candidate":
		var c domain.Candidate
		if err := r.tx.Get("candidate", ref.ID, &c); err != nil {
			return f, err
		}
		f.Reference.Version, f.Reference.AuthorizationVersion = c.Version, c.AuthorizationVersion
		f.Title, f.State, f.NextAction = "集成候选", c.State, "view_candidate"
		return r.seal(f, struct{ Commit, Main, Head string }{c.Commit, c.Main, c.Head})
	case "decision":
		return r.decisionFact(ref)
	case "consultation", "initialization", "goal":
		return r.leaderFact(ref)
	}
	return f, domain.ErrNotFound
}

func (r workReader) projectFact(ref domain.WorkReference) (domain.WorkFact, error) {
	if ref.ID != r.project.ID {
		return domain.WorkFact{}, domain.ErrNotFound
	}
	d, err := goalDocument(r.tx)
	if err != nil {
		return domain.WorkFact{}, err
	}
	i, err := initialization(r.tx)
	if err != nil {
		return domain.WorkFact{}, err
	}
	f := domain.WorkFact{Reference: domain.WorkReference{Kind: "project", ID: ref.ID, Version: r.project.PolicyVersion,
		AuthorizationVersion: d.AuthorizationVersion, Revision: i.Version}, State: "active", NextAction: "view_project"}
	if r.project.Paused {
		f.State = "paused"
	}
	workDescription(&f, r.project.Name, "")
	return r.seal(f, struct {
		Goal, TestPolicy int64
		Baseline         string
	}{d.Version, r.project.TestPolicyVersion, i.ApprovedDigest})
}

func (r workReader) runFact(ref domain.WorkReference) (domain.WorkFact, error) {
	var v domain.Run
	f := domain.WorkFact{Reference: domain.WorkReference{Kind: "run", ID: ref.ID}}
	if err := r.tx.Get("run", ref.ID, &v); err != nil {
		return f, err
	}
	if r.nodeBound() && v.NodeID != r.binding.NodeID {
		return f, domain.ErrNotFound
	}
	f.Reference.Version, f.Reference.AuthorizationVersion, f.Reference.Generation = v.Version, v.AuthorizationVersion, v.Generation
	f.Title, f.State, f.NextAction = redacted(v.Tool), v.State, "view_run"
	var t domain.Task
	f.Stale = r.tx.Get("task", v.TaskID, &t) != nil || !currentChatTask(t, v)
	if !f.Stale {
		workDescription(&f, t.Title, t.Description)
	}
	return r.seal(f, struct {
		Base                string
		Goal, GoalAuthority int64
		Quiet               bool
	}{v.Base, v.GoalVersion, v.GoalAuthorizationVersion, v.Quiescent})
}

func (r workReader) decisionFact(ref domain.WorkReference) (domain.WorkFact, error) {
	var d domain.Decision
	f := domain.WorkFact{Reference: domain.WorkReference{Kind: "decision", ID: ref.ID}}
	if err := r.tx.Get("decision", ref.ID, &d); err != nil {
		return f, err
	}
	goal, err := goalDocument(r.tx)
	if err != nil {
		return f, err
	}
	f.Reference.Version, f.Reference.AuthorizationVersion = d.Version, d.AuthorizationVersion
	f.Stale = d.DocumentVersion != 0 && (d.DocumentVersion != goal.Version || d.AuthorizationVersion != goal.AuthorizationVersion)
	f.State, f.NextAction = d.Status, "view_decision"
	workDescription(&f, d.Kind, d.Evidence)
	if d.Status == "open" && !f.Stale {
		f.NextAction = "owner_decision"
		f.Questions = []domain.WorkQuestion{{ID: d.ID, Text: f.Text, State: "awaiting_owner", NextAction: "view_decision"}}
	}
	return r.seal(f, struct {
		Source        string
		Version, Goal int64
	}{d.SourceID, d.SourceVersion, goal.Version})
}
