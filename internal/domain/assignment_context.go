package domain

import "encoding/json"

// Leave room for execution instructions and Run metadata in the 256 KiB prompt.
const MaxAssignmentContextBytes = 240 << 10

func ValidateAssignmentContext(a Assignment) error {
	if a.FailureRecoveryVersion < 0 || a.FailureRecoveryVersion > 1 {
		return Fail("invalid_context", "unsupported failure recovery contract version")
	}
	if a.Task.FailureAssessmentID != "" {
		if a.FailureRecoveryVersion != 1 || a.FailureContext == nil ||
			a.FailureContext.AssessmentID != a.Task.FailureAssessmentID || a.FailureContext.SourceTaskID != a.Task.FailureSourceTaskID ||
			a.FailureContext.SourceRunID == "" {
			return Fail("invalid_context", "follow-up requires its fixed failure source context")
		}
	}
	if a.Task.FailureRecheckID != "" && a.FailureContext != nil &&
		(a.FailureContext.AssessmentID != a.Task.FailureRecheckID || a.FailureContext.SourceTaskID != a.Task.ID) {
		return Fail("invalid_context", "recheck context must identify its original task")
	}
	if a.Candidate != nil && a.Candidate.ReviewRules != nil {
		if err := ValidateReviewRules(*a.Candidate.ReviewRules, a.Candidate.Main); err != nil {
			return err
		}
		if a.Run.Kind == "review" && (a.Run.ReviewContractVersion != ReviewContractVersion || a.Run.ReviewRulesDigest != a.Candidate.ReviewRules.Digest) {
			return Fail("invalid_context", "review assignment must negotiate its exact trusted rules")
		}
	}
	if err := ValidateAssignmentRequirements(a); err != nil {
		return err
	}
	var feedback string
	var review *Review
	if a.Candidate != nil {
		feedback, review = a.Candidate.Detail, a.Candidate.Review
	}
	raw, err := json.Marshal(struct {
		Rules         *ReviewRules
		ReviewContext *ReviewContext
		Failure       *FailureContext
		Task          Task
		Baseline      *Baseline
		Goal          *GoalContext
		Requirements  []RequirementContext
		Protected     []string
		Feedback      string
		Review        *Review
	}{assignmentReviewRules(a), a.ReviewContext, a.FailureContext, a.Task, a.Baseline, a.GoalContext, a.Requirements, a.Project.ProtectedPaths, feedback, review})
	if err != nil {
		return err
	}
	if len(raw) > MaxAssignmentContextBytes {
		return Fail("context_limit", "完整任务上下文超过上限，需要拆分任务；未省略需求、验收或权限")
	}
	return nil
}

func assignmentReviewRules(a Assignment) *ReviewRules {
	if a.Candidate != nil {
		return a.Candidate.ReviewRules
	}
	return nil
}

// ValidateAssignmentRequirements validates fixed transport data, not new
// authority. A newer Runner cannot silently execute IDs without their content.
func ValidateAssignmentRequirements(a Assignment) error {
	expected := map[string]bool{}
	for _, id := range a.Task.RequirementIDs {
		if id == "" || expected[id] {
			return Fail("invalid_context", "任务需求绑定无效")
		}
		expected[id] = true
	}
	if len(expected) != len(a.Requirements) {
		return Fail("context_required", "缺少固定版本需求正文，请先升级中心服务")
	}
	seen := map[string]bool{}
	for _, req := range a.Requirements {
		revision := a.Run.RequirementRevisions[req.ID]
		if revision == 0 {
			revision = 1
		}
		if !expected[req.ID] || seen[req.ID] || req.ContentRevision != revision {
			return Fail("invalid_context", "需求正文与执行绑定不匹配")
		}
		if err := ValidateRequirementProposal(RequirementProposal{Title: req.Title, Body: req.Body}); err != nil {
			return err
		}
		if req.State != "" && req.State != "active" && req.State != "withdrawn" {
			return Fail("invalid_context", "需求状态无效")
		}
		seen[req.ID] = true
	}
	for id := range a.Run.RequirementRevisions {
		if !expected[id] {
			return Fail("invalid_context", "执行包含无关需求绑定")
		}
	}
	return nil
}
