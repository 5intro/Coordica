package domain

// The finite allowance belongs to one authorized goal, never service lifetime.
// Its revision is separate from the immutable goal content/authorization tuple.
type GoalBudgetRequest struct {
	ExpectedVersion              int64  `json:"expected_version"`
	ExpectedGoalVersion          int64  `json:"expected_goal_version"`
	ExpectedAuthorizationVersion int64  `json:"expected_authorization_version"`
	BudgetSeconds                int64  `json:"budget_seconds"`
	Reason                       string `json:"reason"`
}

type GoalBudgetState struct {
	Version                  int64  `json:"version"`
	GoalVersion              int64  `json:"goal_version"`
	GoalAuthorizationVersion int64  `json:"goal_authorization_version"`
	BudgetSeconds            int64  `json:"budget_seconds"`
	UsedSeconds              int64  `json:"used_seconds"`
	ReservedSeconds          int64  `json:"reserved_seconds"`
	Reason                   string `json:"reason,omitempty"`
}
