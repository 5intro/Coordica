package runner

import (
	"context"
	"errors"
	"fmt"

	"coordica/internal/agent"
	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/process"
)

// An invalid output is not a review verdict or a request to change code. Reuse
// the center's finite platform retries, but only with affirmative stop evidence
// and a freshly verified unchanged candidate. Never synthesize an approval.
func (r *Runner) reviewProtocolFailure(ctx context.Context, dir string, base gitops.OID,
	result agent.Result, completion *domain.CompleteRequest, cause error,
) error {
	if ctx.Err() != nil {
		return errors.Join(cause, ctx.Err())
	}
	if !result.TreeStopped || !completion.Quiescent || errors.Is(cause, process.ErrTreeNotStopped) {
		return cause
	}
	head, err := r.git.Head(ctx, dir)
	if err == nil && head != base {
		err = errors.New("reviewer changed the fixed candidate")
	}
	if err == nil {
		err = r.git.VerifyClean(ctx, dir, base)
	}
	if err != nil {
		return errors.Join(cause, fmt.Errorf("cannot retry review on an unverified workspace: %w", err))
	}
	completion.Status = "transient_failure"
	return fmt.Errorf("review output contract rejected; unchanged candidate requires a fresh review: %w", cause)
}
