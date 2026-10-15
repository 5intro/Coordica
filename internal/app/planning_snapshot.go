package app

import (
	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/store"
	"errors"
)

func phaseBSnapshot(tx *store.Tx, v *domain.Snapshot) error {
	var e error
	v.Conversations, e = store.List[domain.ConversationRequest](tx, "conversation")
	if e != nil {
		return e
	}
	var account domain.ConsultationAccount
	if e = tx.Get("consultation_account", "project", &account); e == nil {
		v.ConsultationAccount = &account
	} else if !errors.Is(e, domain.ErrNotFound) {
		return e
	}
	v.Automation, e = automation(tx)
	if e != nil {
		return e
	}
	var d domain.PlanningDocument
	e = tx.Get("planning_document", "current", &d)
	if e == nil {
		v.PlanningDocument = &d
	} else if !errors.Is(e, domain.ErrNotFound) {
		return e
	}
	if v.LeaderMessages, e = store.List[domain.LeaderMessage](tx, "leader_message"); e != nil {
		return e
	}
	if v.PlanningRuns, e = store.List[domain.PlanningRun](tx, "planning_run"); e != nil {
		return e
	}
	if v.Decisions, e = store.List[domain.Decision](tx, "decision"); e != nil {
		return e
	}
	if v.ScopeRequests, e = store.List[domain.ScopeAssessment](tx, "scope_request"); e != nil {
		return e
	}
	v.Blocks = []domain.BlockItem{}
	for _, t := range v.Tasks {
		if t.HoldReason != "" {
			v.Blocks = append(v.Blocks, domain.BlockItem{Kind: "task", ObjectID: t.ID, Reason: t.HoldReason, Actions: []string{"inspect_task"}})
		}
	}
	for _, r := range v.Runs {
		if !r.Quiescent && r.State == "lost" {
			v.Blocks = append(v.Blocks, domain.BlockItem{Kind: "run", ObjectID: r.ID, Reason: "unknown_live", Actions: []string{"confirm_stopped"}})
		}
	}
	for _, c := range v.Candidates {
		current := false
		if v.Project != nil {
			for _, t := range v.Tasks {
				if candidateAuthority(tx, t, *v.Project, c) {
					current = true
				}
			}
		}
		if current && c.State == "ready_to_land" {
			blockedTests, e := blockingTestFailures(tx, c.Main, c.Commit)
			if e != nil {
				return e
			}
			if len(blockedTests) > 0 {
				v.Blocks = append(v.Blocks, domain.BlockItem{Kind: "candidate", ObjectID: c.ID, Reason: "critical_test_failure", Actions: []string{"inspect_test", "create_repair"}})
			}
		}
		if current && (c.State == "reviewing" || c.State == "validating" || c.State == "ready_to_land") && c.Protected && c.ApprovedCommit != c.Commit && c.Commit != "" && c.State != "integrated" && c.State != "rejected" && c.State != "invalidated" {
			v.Blocks = append(v.Blocks, domain.BlockItem{Kind: "candidate", ObjectID: c.ID, Reason: "protected_candidate", Actions: []string{"approve_exact_candidate"}})
		}
	}
	if v.PlanningDocument != nil && d.Status != "admitted" {
		v.Blocks = append(v.Blocks, domain.BlockItem{Kind: "planning_document", ObjectID: "current", Reason: "planning_not_ready", Actions: []string{"edit_goal", "authorize", "evaluate"}})
	}
	for _, r := range v.PlanningRuns {
		if r.State == "unknown_live" || r.State == "blocked" {
			actions := []string{"inspect_decision"}
			if r.Purpose == domain.ConsultationPurpose {
				actions = []string{"inspect_consultation"}
			}
			v.Blocks = append(v.Blocks, domain.BlockItem{Kind: "planning_run", ObjectID: r.ID, Reason: r.State, Actions: actions})
		}
	}
	for _, d := range v.Decisions {
		if d.Status == "open" {
			v.Blocks = append(v.Blocks, domain.BlockItem{Kind: "decision", ObjectID: d.ID, Reason: d.Kind, Actions: d.Actions})
		}
	}
	return nil
}
func reserveGoal(tx *store.Tx, t domain.Task, grant int64) error {
	if t.GoalVersion == 0 {
		return nil
	}
	var d domain.PlanningDocument
	if e := tx.Get("planning_document", "current", &d); e != nil {
		return e
	}
	d.ReservedSeconds += grant
	return tx.Put("planning_document", "current", d)
}
func settleGoal(tx *store.Tx, t domain.Task, delta, released int64) error {
	if t.GoalVersion == 0 {
		return nil
	}
	var d domain.PlanningDocument
	if e := tx.Get("planning_document", "current", &d); e != nil {
		return e
	}
	d.UsedSeconds += delta
	d.ReservedSeconds -= delta + released
	return tx.Put("planning_document", "current", d)
}

func currentGoalScope(tx *store.Tx) (*gitops.Scope, error) {
	var d domain.PlanningDocument
	e := tx.Get("planning_document", "current", &d)
	if errors.Is(e, domain.ErrNotFound) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	patterns := []string{}
	for _, root := range d.AllowedRoots {
		if root == "." {
			patterns = append(patterns, "**")
		} else {
			patterns = append(patterns, root+"/**")
		}
	}
	return gitops.NewScope(patterns, nil)
}

func assignedGoalContext(tx *store.Tx) (*domain.GoalContext, error) {
	var d domain.PlanningDocument
	e := tx.Get("planning_document", "current", &d)
	if errors.Is(e, domain.ErrNotFound) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	return &domain.GoalContext{Version: d.Version, AuthorizationVersion: d.AuthorizationVersion, Objective: d.Objective, NonGoals: d.NonGoals, Acceptance: d.Acceptance, Architecture: d.Architecture, DirectoryOutline: d.DirectoryOutline, AllowedRoots: d.AllowedRoots}, nil
}

func candidateAuthority(tx *store.Tx, t domain.Task, p domain.Project, c domain.Candidate) bool {
	return c.BaselineVersion == t.BaselineVersion && c.BaselineDigest == t.BaselineDigest && !p.Paused && t.State != "cancelled" && t.State != "integrated" && t.HoldReason == "" && t.ID == c.TaskID && t.CurrentCandidateID == c.ID && t.AuthorizationVersion == c.AuthorizationVersion && p.PolicyVersion == c.PolicyVersion && goalGate(tx, &t, false) == nil
}
