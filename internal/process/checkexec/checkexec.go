// Package checkexec runs finite command suites. It knows no tasks, database,
// HTTP, Git state or model protocol; callers supply the isolated workspace.
package checkexec

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"coordica/internal/domain"
	"coordica/internal/process"
)

func Run(ctx context.Context, dir string, checks []domain.Check, env []string) ([]domain.CheckResult, bool, error) {
	results := make([]domain.CheckResult, 0, len(checks))
	for _, check := range checks {
		if ctx.Err() != nil {
			return results, true, ctx.Err()
		}
		if err := requiredInputs(dir, check); err != nil {
			results = append(results, domain.CheckResult{Name: check.Name, ExitCode: -1, Output: err.Error()})
			return results, true, err
		}
		started := time.Now()
		checkCtx, cancel := context.WithTimeout(ctx, time.Duration(check.TimeoutSeconds)*time.Second)
		result := domain.CheckResult{Name: check.Name, ExitCode: -1}
		session, err := process.Start(checkCtx, process.Spec{Executable: check.Command, Args: check.Args, Dir: dir, Env: env, GracePeriod: 2 * time.Second, DrainTimeout: 2 * time.Second})
		kind := "check_start"
		quiet := !errors.Is(err, process.ErrTreeNotStopped)
		if err == nil {
			kind = "check_failed"
			var output strings.Builder
			for event := range session.Output() {
				remaining := (256 << 10) - output.Len()
				if remaining > 0 {
					line := string(event.Line) + "\n"
					if len(line) > remaining {
						line = line[:remaining]
					}
					output.WriteString(line)
				}
			}
			outcome, waitErr := session.Wait()
			quiet = outcome.TreeStopped
			result.ExitCode = outcome.ExitCode
			result.Output = output.String()
			result.Passed = waitErr == nil && outcome.ExitCode == 0 && quiet && checkCtx.Err() == nil
			err = waitErr
		} else {
			result.Output = err.Error()
		}
		if checkCtx.Err() != nil {
			err = checkCtx.Err()
			kind = "check_timeout"
		}
		cancel()
		result.DurationSeconds = int64(time.Since(started) / time.Second)
		results = append(results, result)
		if !result.Passed {
			if err == nil {
				err = fmt.Errorf("command exited unsuccessfully")
			}
			return results, quiet, &Failure{Kind: kind, Check: check.Name,
				Cause: fmt.Errorf("exit %d: %w", result.ExitCode, err)}
		}
	}
	return results, true, nil
}
