// Package onboarding bridges one MCP initialization to a persistent user-level
// Runner. Credentials stay in private Runner state, never in returned results.
package onboarding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"coordica/internal/agent"
	"coordica/internal/filelock"
	"coordica/internal/localfile"
	"coordica/internal/nodeservice"
	"coordica/internal/runner"
)

type Config struct {
	CenterURL, PairingCode, TLSCA, DataDir, Name string
	Capacity                                     int
	Executables                                  map[string]string
	Tools                                        []agent.Definition
}
type Result struct {
	NodeID     string             `json:"node_id"`
	Registered bool               `json:"registered"`
	Service    nodeservice.Status `json:"service"`
	Steps      []string           `json:"steps"`
}
type Installer interface {
	EnsureStarted(context.Context, nodeservice.Spec) (nodeservice.Status, error)
}

func Initialize(ctx context.Context, config Config) (Result, error) {
	return InitializeWith(ctx, config, nodeservice.Manager{})
}

// InitializeWith preserves the production executable boundary while allowing
// tests to replace only service activation. It never invokes models.
func InitializeWith(ctx context.Context, config Config, installer Installer) (result Result, err error) {
	result.Service.State = "failed"
	secrets := []string{config.PairingCode}
	defer func() {
		if err != nil {
			raw, _ := json.Marshal(err.Error())
			safe := agent.SanitizeEvent(agent.Event{Type: "error", Data: raw}, secrets...)
			var detail string
			json.Unmarshal(safe.Data, &detail)
			if len(detail) > 2048 {
				detail = detail[:2048]
			}
			err = errors.New(detail)
		}
	}()
	if installer == nil {
		return result, errors.New("service installer is required")
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if config.DataDir == "" {
		return result, errors.New("fixed Runner data directory is required")
	}
	data, err := filepath.Abs(config.DataDir)
	if err != nil {
		return result, err
	}
	if err = os.MkdirAll(data, 0700); err != nil {
		return result, err
	}
	if err = localfile.Protect(data); err != nil {
		return result, err
	}
	release, err := filelock.Acquire(filepath.Join(data, "onboarding.lock"))
	if err != nil {
		return result, errors.New("another initialization owns this Runner directory")
	}
	defer release()
	center, err := normalizedCenter(config.CenterURL)
	if err != nil {
		return result, err
	}
	current, loadErr := runner.Load(data)
	if loadErr == nil {
		secrets = append(secrets, current.Token)
		existing, err := normalizedCenter(current.CenterURL)
		if err != nil {
			return result, errors.New("existing Runner center configuration is invalid")
		}
		if existing != center {
			return result, errors.New("Runner directory is already paired with a different center")
		}
		if current.NodeID == "" || current.Token == "" {
			return result, errors.New("existing Runner identity is incomplete; refusing to overwrite it")
		}
		result.NodeID, current.DataDir = current.NodeID, data
		result.Registered = true
		result.Steps = append(result.Steps, "existing_registration_reused")
		unlockRunner, lockErr := filelock.Acquire(filepath.Join(data, "runner.lock"))
		if lockErr != nil {
			// Replacing a service definition can restart a working Runner. Never
			// touch either its configuration or its manager without this lock.
			result.Service.State = "directory_in_use"
			result.Steps = append(result.Steps, "runtime_configuration_reused_while_running", "service_unchanged", "stop_current_execution_before_retry")
			return result, errors.New("Runner directory cannot be exclusively locked; identity retained and service unchanged; stop current execution before retrying")
		}
		current, err = refreshRuntime(config, current)
		unlockRunner() // Release before the independent service acquires it.
		if err != nil {
			return result, fmt.Errorf("Runner identity retained; local runtime configuration was not refreshed: %w", err)
		}
		result.Steps = append(result.Steps, "runtime_configuration_refreshed")
	} else if !errors.Is(loadErr, os.ErrNotExist) {
		return result, errors.New("existing Runner configuration cannot be read; refusing to overwrite it")
	} else {
		if strings.TrimSpace(config.PairingCode) == "" {
			return result, errors.New("one-time pairing code is required for first initialization")
		}
		ca := config.TLSCA
		if ca != "" {
			ca, err = filepath.Abs(ca)
			if err != nil {
				return result, err
			}
		}
		executables, err := resolveExecutables(config.Tools, config.Executables)
		if err != nil {
			return result, err
		}
		r, err := runner.New(runner.Config{CenterURL: config.CenterURL, DataDir: data, Name: config.Name, TLSCA: ca, RuntimePath: os.Getenv("PATH"), Capacity: config.Capacity, Executables: executables, Tools: config.Tools})
		if err != nil {
			return result, err
		}
		r.OnEvent = func(runner.Event) {} // The MCP result reports steps, without diagnostic credentials.
		if err = r.Register(ctx, config.PairingCode); err != nil {
			return result, fmt.Errorf("Runner registration failed: %w", err)
		}
		current = r.Config()
		secrets = append(secrets, current.Token)
		result.NodeID = current.NodeID
		result.Registered = true
		result.Steps = append(result.Steps, "runtime_detected", "registration_saved")
	}
	executable, err := os.Executable()
	if err != nil {
		return result, errors.New("current Coordica executable cannot be resolved")
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return result, err
	}
	result.Service, err = installer.EnsureStarted(ctx, nodeservice.Spec{Executable: executable, DataDir: data, RuntimePath: current.RuntimePath})
	if err != nil {
		result.Service.State = "failed"
		result.Steps = append(result.Steps, "service_activation_failed")
		return result, fmt.Errorf("Runner configuration retained; service activation failed: %w", err)
	}
	if result.Service.State != "started" && result.Service.State != "activation_requested" {
		result.Service.State = "failed"
		return result, errors.New("service installer did not confirm activation")
	}
	result.Steps = append(result.Steps, "user_service_activated")
	return result, nil
}

// refreshRuntime runs only with runner.lock held. It refreshes machine-local
// inputs, preserving identity, scheduling limits and every durable handoff.
func refreshRuntime(config Config, current runner.Config) (runner.Config, error) {
	candidate := current
	candidate.RuntimePath = os.Getenv("PATH")
	var err error
	if config.Tools != nil {
		candidate.Tools = config.Tools
	}
	// Re-entry through a default bootstrap must retain local custom drivers and
	// their executable paths; only newly discovered or explicit paths replace them.
	requested := make(map[string]string, len(current.Executables)+len(config.Executables))
	registry, err := runner.LocalRegistry(candidate.Tools)
	if err != nil {
		return current, err
	}
	for id, path := range current.Executables {
		if _, registered := registry.Resolve(id); registered {
			requested[id] = path
		}
	}
	for id, path := range config.Executables {
		requested[id] = path
	}
	candidate.Executables, err = resolveExecutables(candidate.Tools, requested)
	if err != nil {
		return current, err
	}
	if config.TLSCA != "" {
		candidate.TLSCA, err = filepath.Abs(config.TLSCA)
		if err != nil {
			return current, err
		}
	}
	// This validates the public CA and transport settings without a new
	// registration, a capability probe or a model call.
	if _, err := runner.New(candidate); err != nil {
		return current, err
	}
	raw, err := json.MarshalIndent(candidate, "", "  ")
	if err != nil {
		return current, err
	}
	if err := localfile.WritePrivate(filepath.Join(candidate.DataDir, "runner.json"), raw); err != nil {
		return current, err
	}
	return candidate, nil
}

func resolveExecutables(tools []agent.Definition, requested map[string]string) (map[string]string, error) {
	registry, err := runner.LocalRegistry(tools)
	if err != nil {
		return nil, err
	}
	executables, err := runner.NormalizeExecutables(registry, requested)
	if err != nil {
		return nil, err
	}
	for _, definition := range registry.Definitions() {
		choices := definition.Executables
		if path := executables[definition.ID]; path != "" {
			choices = []string{path}
		}
		for _, path := range choices {
			if resolved, lookupErr := exec.LookPath(path); lookupErr == nil {
				absolute, err := filepath.Abs(resolved)
				if err != nil {
					return nil, err
				}
				executables[definition.ID] = absolute
				break
			}
		}
	}
	return executables, nil
}

func normalizedCenter(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("center must be an HTTP(S) URL without credentials, query or fragment")
	}
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}
