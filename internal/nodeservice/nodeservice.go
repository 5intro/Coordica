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

	"coordica/internal/localfile"
	"coordica/internal/process"
)

type Spec struct{ Executable, DataDir, RuntimePath string }
type Status struct {
	Name  string `json:"name"`
	State string `json:"state"`
}
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
	return install(ctx, spec, info, runCommand)
}

type runtimeInfo struct{ Platform, Home, ConfigHome, UID, SID, Path, Manager string }
type command struct {
	Program string
	Args    []string
}
type commandRunner func(context.Context, command) error
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

func runCommand(ctx context.Context, c command) error {
	callCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	_, result, err := process.Collect(callCtx, process.Spec{Executable: c.Program, Args: c.Args}, 64<<10)
	if err != nil {
		return fmt.Errorf("%s failed (exit %d)", filepath.Base(c.Program), result.ExitCode)
	}
	return nil
}
func install(ctx context.Context, spec Spec, info runtimeInfo, run commandRunner) (Status, error) {
	p, err := buildPlan(spec, info)
	if err != nil {
		return Status{State: "failed"}, err
	}
	status := Status{Name: p.Name, State: "failed"}
	if err := run(ctx, p.Preflight); err != nil {
		return status, fmt.Errorf("user service manager unavailable: %w", err)
	}
	old, readErr := os.ReadFile(p.ConfigPath)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return status, errors.New("cannot read existing user service definition")
	}
	changed := string(old) != string(p.Content)
	existed := readErr == nil
	loaded := false
	if info.Platform == "darwin" {
		loaded = run(ctx, p.Probe) == nil
		if loaded && changed {
			if err := run(ctx, p.Reload); err != nil {
				return status, fmt.Errorf("cannot replace existing Runner service: %w", err)
			}
			loaded = false
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
			if err := run(ctx, p.Enable); err != nil {
				return status, fmt.Errorf("cannot install launchd Runner agent: %w", err)
			}
		}
		if err := run(ctx, p.Start); err != nil {
			return status, fmt.Errorf("cannot start launchd Runner agent: %w", err)
		}
		if err := run(ctx, p.Verify); err != nil {
			return status, fmt.Errorf("launchd activation not confirmed: %w", err)
		}
		status.State = "activation_requested"
	case "linux":
		if changed {
			if err := run(ctx, p.Reload); err != nil {
				return status, fmt.Errorf("cannot reload systemd user units: %w", err)
			}
		}
		if err := run(ctx, p.Enable); err != nil {
			return status, fmt.Errorf("cannot enable systemd Runner: %w", err)
		}
		start := p.Start
		if changed && existed {
			start = p.Restart
		}
		if err := run(ctx, start); err != nil {
			return status, fmt.Errorf("cannot start systemd Runner: %w", err)
		}
		if err := run(ctx, p.Verify); err != nil {
			return status, fmt.Errorf("systemd Runner is not active: %w", err)
		}
		status.State = "started"
	case "windows":
		if err := run(ctx, p.Enable); err != nil {
			return status, fmt.Errorf("cannot register current-user Runner task: %w", err)
		}
		if err := run(ctx, p.Start); err != nil {
			return status, fmt.Errorf("cannot activate current-user Runner task: %w", err)
		}
		status.State = "activation_requested"
	default:
		return status, errors.New("unsupported user-service platform")
	}
	return status, nil
}
