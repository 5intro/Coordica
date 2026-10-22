package main

import "coordica/internal/domain"

func publicTypes() []any {
	types := publicPreparationTypes()
	types = append(types, publicRuntimeTypes()...)
	types = append(types, domain.PublicationPolicy{},
		domain.PublicationRequest{},
		domain.RemoteCheck{},
		domain.RemoteReview{},
		domain.PullRequestStatus{})
	types = append(types, domain.TestSuite{},
		domain.TestSuiteSave{},
		domain.VerificationSpec{},
		domain.VerificationRequest{},
		domain.TestReport{},
		domain.CheckPlan{},
		domain.TestReadiness{},
		domain.RequirementLinkRequest{})
	types = append(types, domain.RequirementContext{},
		domain.RequirementEdit{},
		domain.RequirementRevision{},
		domain.RequirementImpact{},
		domain.RequirementChange{},
		domain.RequirementDetail{},
		domain.RequirementTaskUpdate{},
		domain.RequirementNewTask{},
		domain.RequirementPlan{})
	types = append(types, domain.LeaderExecutionRef{},
		domain.LeaderExecution{},
		domain.LeaderActivityEvent{},
		domain.LeaderActivityPage{})
	types = append(types, domain.PlanningAnswerReceipt{})
	types = append(types, publicTestCatalogTypes()...)
	return types
}

func publicTestCatalogTypes() []any {
	return []any{domain.CriterionRef{}, domain.TestCase{}, domain.TestCaseSave{}, domain.TestCaseQuery{}, domain.TestCasePage{},
		domain.AcceptanceCriterion{}, domain.AcceptancePlan{}, domain.AcceptanceSave{}, domain.TestCaseSummary{}, domain.CaseEvidence{},
		domain.CriterionCoverage{}, domain.AcceptanceCoverage{}, domain.TestGap{}, domain.TestSupplementRequest{}, domain.TestSupplementPlan{}}
}

func publicPreparationTypes() []any {
	return []any{
		domain.InitializationOperation{},
		domain.UserServiceStatus{},
		domain.HostLifecycleStatus{},
		domain.CenterHealth{},
		domain.CenterIdentity{},
		domain.CenterRuntimeStatus{},
		domain.ProjectPolicyRequest{},
		domain.Baseline{},
		domain.AnalysisEvidence{},
		domain.InitializationQuestion{},
		domain.InitializationResult{},
		domain.InitializationFeedback{},
		domain.Initialization{},
		domain.InitializationRequest{},
		domain.InitializationAnswer{},
		domain.InitializationConfirm{},
		domain.ChatSource{},
		domain.ChatCredentialRequest{},
		domain.ChatCredentialInfo{},
		domain.ChatCredentialGrant{},
		domain.ConversationInput{},
		domain.ChatRequirementInput{},
		domain.ConsultResult{},
		domain.ConversationRequest{},
		domain.ConsultationAccount{},
		domain.ChatOverview{},
		domain.ChatListQuery{},
		domain.ChatItemQuery{},
		domain.ChatRequestQuery{},
		domain.ChatCancelRequest{},
		domain.ChatAgentCapability{},
		domain.ChatRun{},
		domain.ChatItem{},
		domain.ChatPage{},
		domain.ChatDetail{},
	}
}

func publicRuntimeTypes() []any {
	types := []any{
		domain.ExecutionIntent{},
		domain.RunScopeRequest{},
		domain.ScopeAssessment{},
		domain.AutomationSettings{},
		domain.AutomationRequest{},
		domain.PlanningDocument{},
		domain.GoalContext{},
		domain.PlanningSaveRequest{},
		domain.LeaderMessage{},
		domain.PlanningMessageRequest{},
		domain.TaskProposal{},
		domain.ScopeProposal{},
		domain.MajorDecisionProposal{},
		domain.LeaderResult{},
		domain.PlanningRun{},
		domain.ProposalTaskMapping{},
		domain.Decision{},
		domain.DecisionResolveRequest{},
		domain.ModelOption{},
		domain.ToolScheduling{},
		domain.NodeConfigureRequest{},
		domain.BlockItem{},
		domain.ConnectionInfo{},
		domain.CenterEntry{},
		domain.LocalInfo{},
		domain.Check{},
		domain.Project{},
		domain.Requirement{},
		domain.RequirementProposal{},
		domain.NodeSnapshot{},
		domain.Task{},
		domain.Capability{},
		domain.Node{},
		domain.Run{},
		domain.Review{},
		domain.CheckResult{},
		domain.Candidate{},
		domain.Event{},
		domain.Snapshot{},
		domain.VersionRequest{},
		domain.ScopeRequest{},
		domain.RegisterRequest{},
		domain.RegisterResponse{},
		domain.ClaimRequest{},
		domain.Assignment{},
		domain.HeartbeatRequest{},
		domain.HeartbeatResponse{},
		domain.CompleteRequest{},
		domain.Problem{},
		domain.StopConfirmation{},
		domain.BudgetRequest{},
		domain.Artifact{},
	}
	return types
}
