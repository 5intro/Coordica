package gitops

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"coordica/internal/process"
)

func gitCommandSpec(g *Client, dir string, extra []string, args ...string) (process.Spec, error) {
	if err := validateWorkingDirectory(dir); err != nil {
		return process.Spec{}, err
	}
	argv := []string{"--no-pager"}
	if dir != "" && runtime.GOOS != "windows" {
		argv = append(argv, "-C", dir)
	}
	argv = append(argv, args...)
	spec := process.Spec{Executable: g.GitPath, Args: argv, Env: controlledEnv(extra), GracePeriod: 750 * time.Millisecond, DrainTimeout: 2 * time.Second}
	if dir != "" && runtime.GOOS == "windows" {
		// Git processes -C before reading core.longpaths. Let Go set the
		// native working directory instead, preserving relative Git paths.
		if !filepath.IsAbs(spec.Executable) {
			absolute, err := exec.LookPath(spec.Executable)
			if err != nil {
				return process.Spec{}, err
			}
			absolute, err = filepath.Abs(absolute)
			if err != nil {
				return process.Spec{}, err
			}
			spec.Executable = absolute
		}
		spec.Dir = dir
	}
	return spec, nil
}

func gitCommandFailure(ctx context.Context, op string, stderr []byte, result process.Result, err error) error {
	if ctx.Err() != nil {
		// Context cancellation must not hide a failed tree-stop observation.
		err = errors.Join(err, ctx.Err())
	}
	if errors.Is(err, process.ErrOutputLimit) {
		return &Error{Op: op, Kind: errors.Join(ErrOutputLimit, err), Detail: "managed command output limit exceeded", ExitCode: result.ExitCode}
	}
	if err != nil {
		e := &Error{Op: op, Kind: errors.Join(ErrGit, err), Detail: strings.TrimSpace(string(stderr)), ExitCode: result.ExitCode}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			e.ExitCode = exit.ExitCode()
		} else if e.Detail == "" {
			e.Detail = err.Error()
		}
		return e
	}
	return nil
}
