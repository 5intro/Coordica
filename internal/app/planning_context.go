package app

import (
	"context"
	"encoding/json"
	"strings"

	"coordica/internal/codequality"
	"coordica/internal/domain"
	"coordica/internal/store"
)

type planningFacts struct {
	TestSuites             []domain.TestSuite           `json:"test_suites"`
	Document               domain.PlanningDocument      `json:"document"`
	Messages               []domain.LeaderMessage       `json:"messages"`
	Tasks                  []domain.Task                `json:"-"`
	TaskIndex              []planningTaskIndex          `json:"existing_tasks"`
	Baseline               leaderBaselineContext        `json:"shared_baseline"`
	Materials              leaderMaterials              `json:"materials"`
	History                leaderHistory                `json:"history"`
	ScopeTask              *domain.Task                 `json:"scope_task,omitempty"`
	Requirements           []domain.Requirement         `json:"requirements"`
	Mappings               []domain.ProposalTaskMapping `json:"proposal_mappings"`
	Requests               []domain.ScopeAssessment     `json:"scoped_requests"`
	ProtectedPaths         []string                     `json:"protected_paths"`
	Checks                 []domain.Check               `json:"checks"`
	Resources              []domain.Resource            `json:"resources"`
	Automation             domain.AutomationSettings    `json:"automation"`
	ProjectPolicyVersion   int64                        `json:"project_policy_version"`
	ProjectUsedSeconds     int64                        `json:"project_used_seconds"`
	ProjectReservedSeconds int64                        `json:"project_reserved_seconds"`
}

func (w *PlanningWorker) planningPrompt(ctx context.Context, r domain.PlanningRun, d domain.PlanningDocument, messages []domain.LeaderMessage, dir string) (string, error) {
	var facts planningFacts
	facts.Document = d
	e := w.Service.DB.View(ctx, func(tx *store.Tx) error {
		var err error
		facts.Baseline, err = currentLeaderBaseline(tx, r)
		if err != nil {
			return err
		}
		facts.Tasks, err = store.List[domain.Task](tx, "task")
		if err != nil {
			return err
		}
		facts.Requirements, err = authorizedGoalRequirements(tx, d)
		if err != nil {
			return err
		}
		plans, err := store.List[domain.PlanningRun](tx, "planning_run")
		if err != nil {
			return err
		}
		seenMappings := map[string]bool{}
		for _, p := range plans {
			if p.DocumentVersion == d.Version && p.AuthorizationVersion == d.AuthorizationVersion && p.State == "succeeded" {
				for _, m := range p.TaskMapping {
					if !seenMappings[m.Key] {
						facts.Mappings = append(facts.Mappings, m)
						seenMappings[m.Key] = true
					}
				}
			}
		}
		facts.Requests, err = store.List[domain.ScopeAssessment](tx, "scope_request")
		if err != nil {
			return err
		}
		project, err := getProject(tx)
		if err != nil {
			return err
		}
		facts.Checks = project.Checks
		facts.TestSuites, err = store.List[domain.TestSuite](tx, "test_suite")
		if err != nil {
			return err
		}
		facts.ProjectPolicyVersion = project.PolicyVersion
		facts.ProjectUsedSeconds = project.UsedSeconds
		facts.ProjectReservedSeconds = project.ReservedSeconds
		facts.Resources, err = store.List[domain.Resource](tx, "resource")
		if err != nil {
			return err
		}
		facts.Automation, err = automation(tx)
		if err != nil {
			return err
		}
		facts.ProtectedPaths = project.ProtectedPaths
		return nil
	})
	if e != nil {
		return "", e
	}
	return buildPlanningContext(dir, r, facts, messages)
}

func buildPlanningContext(dir string, r domain.PlanningRun, facts planningFacts, messages []domain.LeaderMessage) (string, error) {
	// Prior output and native session identifiers are not standing rules.
	facts.Document.LeaderResult, facts.Document.LeaderSession = nil, ""
	history := currentLeaderHistory(messages, facts.Document.Version)
	var err error
	facts.Materials, err = writeLeaderMaterials(dir, r, facts.Tasks, facts.Requirements, facts.Requests, history, nil)
	if err != nil {
		return "", err
	}
	facts.TaskIndex = indexPlanningTasks(facts.Tasks)
	requests := facts.Requests
	facts.Requests = []domain.ScopeAssessment{}
	for _, request := range requests {
		if request.ID == r.ScopeRequestID {
			facts.Requests = append(facts.Requests, request)
			for i := range facts.Tasks {
				if facts.Tasks[i].ID == request.TaskID {
					task := facts.Tasks[i]
					facts.ScopeTask = &task
				}
			}
		}
	}
	facts.Messages, facts.History, err = selectLeaderHistory(history)
	if err != nil {
		return "", err
	}
	instruction := strings.TrimSuffix(planningInstruction, " Input: ") + codequality.Instructions + leaderMaterialInstruction
	if r.ScopeRequestID != "" {
		instruction += " Assess ONLY scoped request " + r.ScopeRequestID + ". Return tasks []. A successful ordinary scope proposal must extend the original task's allowed paths with the requested paths inside the literal authorized roots, preserving all existing permissions. No product, architecture, checks or goal changes. Return exactly one scope for the request task with its current version, or actionable major_decisions."
	}
	// Older conversation is optional; contracts and the newest request are not.
	for {
		raw, err := json.Marshal(facts)
		if err != nil {
			return "", err
		}
		prompt := instruction + " Input: " + string(raw)
		if len(prompt) <= leaderPromptLimit {
			return prompt, nil
		}
		if len(facts.Messages) <= 1 {
			return "", domain.Fail("context_limit", "完整规划约束与当前请求超过输入上限，请拆分目标；未省略权限或验收条件")
		}
		removed := facts.Messages[0].ID
		truncated := facts.History.Truncated[:0]
		for _, id := range facts.History.Truncated {
			if id != removed {
				truncated = append(truncated, id)
			}
		}
		facts.History.Truncated = truncated
		facts.Messages = facts.Messages[1:]
		facts.History.Omitted++
	}
}
