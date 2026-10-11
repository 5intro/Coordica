//go:build !windows

package process

import "os/exec"

func commandFor(spec Spec) (*exec.Cmd, error) {
	cmd := exec.Command(spec.Executable, spec.Args...)
	return cmd, cmd.Err
}
