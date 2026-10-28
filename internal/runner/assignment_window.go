package runner

import (
	"fmt"
	"strings"
	"time"

	"coordica/internal/domain"
)

// Execution limits are fixed facts from the assignment, not scheduling grants
// made by the Agent. Keep the per-Run window distinct from cumulative task use.
func appendExecutionWindow(text *strings.Builder, a domain.Assignment) {
	if domain.ProgressManaged(a.Run) {
		fmt.Fprintf(text, "Development has no fixed duration or cumulative allowance by default. Progress observations every %d seconds do not imply completion, failure or new authority.\n", a.Run.ProgressCheckSeconds)
		text.WriteString("The Runner keeps its renewable lease and reports activity without interrupting this turn. Preserve the execution scope, cancellation and structured result contract. Never claim an unrun check passed.\n")
		return
	}
	if a.Run.GrantSeconds <= 0 || a.Run.Deadline.IsZero() {
		return // Compatibility with old fixtures/records that omitted these facts.
	}
	fmt.Fprintf(text, "Run grant: %d seconds; fixed server deadline (UTC): %s.\n",
		a.Run.GrantSeconds, a.Run.Deadline.UTC().Format(time.RFC3339Nano))
	fmt.Fprintf(text, "Task cumulative allowance: %d seconds; used at assignment: %d; reserved at assignment: %d.\n",
		a.Task.BudgetSeconds, a.Task.UsedSeconds, a.Task.ReservedSeconds)
	text.WriteString("The Run grant includes setup, execution and delivery, so less time remains after setup. " +
		"Finish role-appropriate checks, any required cleanup and the structured result before the fixed deadline. " +
		"The task allowance also covers later review, checks and repairs; it is not this Run's time window. " +
		"Lease renewal or model prose does not extend the Run grant or authorize new time. " +
		"Report actual evidence and unfinished work honestly; never claim an unrun check passed.\n")
}
