// Package nodeservice installs one narrowly defined user-level Runner service.
// It accepts no shell snippets, model commands, credentials or remote executables.
package nodeservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"coordica/internal/domain"
	"coordica/internal/localfile"
	"coordica/internal/process"
)

type Spec struct {
	Executable, DataDir, RuntimePath string
	CenterConfig, HTTPSProxy         string
}
type Status = domain.UserServiceStatus
type Manager struct{}

func (Manager) EnsureStarted(ctx context.Context, spec Spec) (Status, error) {
	return EnsureStarted(ctx, spec)
}

// EnsureStarted requests OS-managed user-level activation. "started" means
// the manager reported an active service; it never means center connectivity.
func EnsureStarted(ctx context.Context, spec Spec) (Status, error) {
	if err := validateSpec(spec); err != nil {
		return Status{State: "failed"}, err
	}
	info, err := nativeRuntime()
	if err != nil {
		return Status{State: "failed"}, err
	}
	if spec.RuntimePath != "" {
		info.Path = spec.RuntimePath
	}
	status, err := install(ctx, spec, info, runCommand)
	if err != nil && spec.CenterConfig != "" {
		return status, fmt.Errorf("center user service activation: %s", strings.ReplaceAll(err.Error(), "Runner", "center"))
	}
	return status, err
}

type runtimeInfo struct{ Platform, Home, ConfigHome, UID, SID, Path, Manager string }
type command struct {
	Program     string
	Args        []string
	WindowsTask string // fixed generated task name; never a command or script
}
type commandRunner func(context.Context, command) (string, error)
type plan struct {
	Name, ConfigPath, LogPath                                string
	Content                                                  []byte
	Preflight, Probe, Reload, Enable, Start, Restart, Verify command
}

func validateSpec(spec Spec) error {
	for _, path := range []string{spec.Executable, spec.DataDir} {
		if !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\r\n") {
			return errors.New("service paths must be absolute and contain no control characters")
		}
	}
	if spec.CenterConfig != "" && (!filepath.IsAbs(spec.CenterConfig) || strings.ContainsAny(spec.CenterConfig, "\x00\r\n")) {
		return errors.New("fixed absolute center configuration required")
	}
	info, err := os.Stat(spec.Executable)
	if err != nil {
		return errors.New("current Coordica executable is unavailable")
	}
	if !info.Mode().IsRegular() {
		return errors.New("current Coordica executable is not a regular file")
	}
	info, err = os.Stat(spec.DataDir)
	if err != nil || !info.IsDir() {
		return errors.New("Runner data directory is unavailable")
	}
	return nil
}
func serviceName(data string) string {
	sum := sha256.Sum256([]byte(data))
	return "coordica-runner-" + hex.EncodeToString(sum[:8])
}

func runCommand(ctx context.Context, c command) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var env []string
	if c.WindowsTask != "" {
		if !windowsOwnedTaskName.MatchString(c.WindowsTask) {
			return "", errors.New("invalid fixed Windows service task name")
		}
		env = []string{}
		for _, value := range os.Environ() {
			key, _, _ := strings.Cut(value, "=")
			if !strings.EqualFold(key, "COORDICA_SERVICE_TASK_NAME") {
				env = append(env, value)
			}
		}
		env = append(env, "COORDICA_SERVICE_TASK_NAME="+c.WindowsTask)
	}
	output, stderr, result, err := process.CollectRaw(callCtx, process.Spec{Executable: c.Program, Args: c.Args, Env: env}, 64<<10, 64<<10)
	if err != nil {
		if errors.Is(err, process.ErrTreeNotStopped) || errors.Is(err, process.ErrOutputLimit) || callCtx.Err() != nil {
			return "", errors.New("user service manager query did not complete with stopped bounded output")
		}
		return "", &managerError{ExitCode: result.ExitCode, Output: string(output) + string(stderr)}
	}
	if c.WindowsTask != "" {
		return decodeWindowsTaskProbe(output, c.WindowsTask)
	}
	return string(output), nil
}
func install(ctx context.Context, spec Spec, info runtimeInfo, run commandRunner) (Status, error) {
	p, err := buildPlan(spec, info)
	if err != nil {
		return Status{State: "failed"}, err
	}
	status := Status{Name: p.Name, State: "failed"}
	if _, err := run(ctx, p.Preflight); err != nil {
		return status, fmt.Errorf("user service manager unavailable: %w", err)
	}
	old, readErr := os.ReadFile(p.ConfigPath)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return status, errors.New("cannot read existing user service definition")
	}
	changed := string(old) != string(p.Content)
	existed := readErr == nil
	if existed && changed {
		return status, errors.New("existing user service definition conflicts; service untouched")
	}
	registered, probeErr := run(ctx, p.Probe)
	loaded := probeErr == nil
	if info.Platform == "linux" && probeErr == nil && strings.Contains(registered, "LoadState=not-found") {
		loaded = false
	}
	if probeErr != nil && !registrationAbsent(info.Platform, probeErr) {
		return status, errors.New("cannot establish registered user service ownership; service untouched")
	}
	if loaded {
		if !existed || !registeredMatches(spec, info, p, registered) {
			return status, errors.New("registered user service identity conflicts or is unknown; service untouched")
		}
	}
	if changed {
		if err := localfile.WritePrivate(p.ConfigPath, p.Content); err != nil {
			return status, fmt.Errorf("cannot save user service definition: %w", err)
		}
	}
	if p.LogPath != "" {
		f, err := os.OpenFile(p.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return status, errors.New("cannot create private Runner service log")
		}
		f.Close()
		if err := localfile.Protect(p.LogPath); err != nil {
			return status, err
		}
	}
	switch info.Platform {
	case "darwin":
		if !loaded {
			if _, err := run(ctx, p.Enable); err != nil {
				return status, fmt.Errorf("cannot install launchd Runner agent: %w", err)
			}
		}
		if _, err := run(ctx, p.Start); err != nil {
			return status, fmt.Errorf("cannot start launchd Runner agent: %w", err)
		}
		if _, err := run(ctx, p.Verify); err != nil {
			return status, fmt.Errorf("launchd activation not confirmed: %w", err)
		}
		status.State = "activation_requested"
	case "linux":
		if changed {
			if _, err := run(ctx, p.Reload); err != nil {
				return status, fmt.Errorf("cannot reload systemd user units: %w", err)
			}
		}
		if _, err := run(ctx, p.Enable); err != nil {
			return status, fmt.Errorf("cannot enable systemd Runner: %w", err)
		}
		start := p.Start
		if changed && existed {
			start = p.Restart
		}
		if _, err := run(ctx, start); err != nil {
			return status, fmt.Errorf("cannot start systemd Runner: %w", err)
		}
		if _, err := run(ctx, p.Verify); err != nil {
			return status, fmt.Errorf("systemd Runner is not active: %w", err)
		}
		status.State = "started"
	case "windows":
		if !loaded {
			if _, err := run(ctx, p.Enable); err != nil {
				return status, fmt.Errorf("cannot register current-user Runner task: %w", err)
			}
		}
		if _, err := run(ctx, p.Start); err != nil {
			return status, fmt.Errorf("cannot activate current-user Runner task: %w", err)
		}
		status.State = "activation_requested"
	default:
		return status, errors.New("unsupported user-service platform")
	}
	return status, nil
}
