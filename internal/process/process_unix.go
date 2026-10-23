//go:build !windows

package process

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

type platformGuard struct{ pgid int }

func prepare(cmd *exec.Cmd) (*platformGuard, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return &platformGuard{}, nil
}
func (g *platformGuard) started(pid int) error { g.pgid = pid; return nil }
func (g *platformGuard) signal(sig syscall.Signal) error {
	if g.pgid <= 0 {
		return nil
	}
	err := syscall.Kill(-g.pgid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
func (g *platformGuard) terminate() error { return g.signal(syscall.SIGTERM) }
func (g *platformGuard) kill() error      { return g.signal(syscall.SIGKILL) }
func (g *platformGuard) alive() bool      { return g.pgid > 0 && groupAlive(g.pgid) }
func (g *platformGuard) waitEmpty(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for g.alive() {
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
	return true
}
func (g *platformGuard) cleanup(grace time.Duration) (bool, error) {
	if !g.alive() {
		return true, nil
	}
	err := g.terminate()
	if g.waitEmpty(grace) {
		return groupStopResult(true, err)
	}
	killErr := g.kill()
	return groupStopResult(g.waitEmpty(time.Second), err, killErr)
}

// A signal can fail while the last group member is exiting. Only an
// independently established empty group can make that EPERM race harmless.
// A permissions error alone is never proof that a managed tree has stopped.
func groupStopResult(stopped bool, signals ...error) (bool, error) {
	var err error
	for _, signalErr := range signals {
		if stopped && signalErr == syscall.EPERM {
			continue
		}
		err = errors.Join(err, signalErr)
	}
	return stopped, err
}
func (g *platformGuard) close() {}

// TreeQuiescent checks the recorded process group. A true result excludes
// ordinary descendants; detached processes remain outside that guarantee.
func TreeQuiescent(pid int) (bool, error) {
	if pid <= 0 {
		return false, errors.New("missing recorded process identity")
	}
	return !groupAlive(pid), nil
}
