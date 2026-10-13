package centerhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"coordica/internal/apiclient"
	"coordica/internal/app"
	"coordica/internal/chatprofile"
	"coordica/internal/domain"
	"coordica/internal/filelock"
	"coordica/internal/localfile"
	"coordica/internal/nodeservice"
	"coordica/internal/store"
)

type Identity = domain.CenterIdentity
type Status = domain.HostLifecycleStatus
type Installer interface {
	EnsureStarted(context.Context, nodeservice.Spec) (nodeservice.Status, error)
}
type Manager struct {
	ConfigPath   string
	Installer    Installer
	Provision    func(context.Context, *app.Service, Config) error
	ReservePorts func(context.Context, []string) (func(), error)
}

func Digest(c Config) string { raw, _ := json.Marshal(c); return app.Hash(string(raw)) }
func (m Manager) Status(ctx context.Context) (Status, error) {
	c, e := Load(m.ConfigPath)
	if e != nil {
		return Status{Role: "center"}, e
	}
	status := Status{Role: "center", Configured: true, ConsoleURL: c.ConsoleURL(), Service: nodeservice.Status{State: "not_requested"}}
	var id Identity
	raw, e := os.ReadFile(filepath.Join(c.DataDir, "center-identity.json"))
	if e == nil && json.Unmarshal(raw, &id) == nil && id.ConfigDigest == Digest(c) && samePath(id.DataDir, c.DataDir) {
		status.Initialized = true
	}
	var operation struct {
		Digest  string             `json:"digest"`
		Service nodeservice.Status `json:"service"`
	}
	if raw, err := os.ReadFile(filepath.Join(c.DataDir, "center-operation.json")); err == nil && json.Unmarshal(raw, &operation) == nil && operation.Digest == Digest(c) {
		status.Service = operation.Service
	}
	if status.Initialized {
		runtime, err := m.runtime(ctx, c, id)
		status.MatchingCenterHealthy = err == nil && m.runnerHealth(ctx, c, id) == nil
		if status.MatchingCenterHealthy {
			status.ProjectPaused = &runtime.ProjectPaused
			status.AutomationEnabled = &runtime.AutomationEnabled
			status.RegisteredRunners = &runtime.RegisteredRunners
			status.LastRunnerHeartbeat = runtime.LastRunnerHeartbeat
			ready := m.verifyProfile(ctx, c) == nil
			status.ChatReady = &ready
		}
	}
	if !status.MatchingCenterHealthy {
		status.Detail = "中心尚未通过匹配数据实例的本地认证健康检查"
	}
	return status, nil
}

// Initialize and Start are the same idempotent lifecycle. They may register a
// new paused project, never redefine an existing project or create a Goal.
func (m Manager) Initialize(ctx context.Context) (Status, error) {
	c, e := Load(m.ConfigPath)
	if e != nil {
		return Status{}, e
	}
	result, _ := m.Status(ctx)
	if _, err := os.Lstat(filepath.Join(c.DataDir, "runner.json")); err == nil {
		return result, errors.New("paired Runner data cannot become a center; choose a separate center instance")
	} else if !os.IsNotExist(err) {
		return result, err
	}
	if e = os.MkdirAll(c.DataDir, 0700); e != nil {
		return result, e
	}
	if e = localfile.Protect(c.DataDir); e != nil {
		return result, e
	}
	unlock, e := filelock.Acquire(filepath.Join(c.DataDir, "center-init.lock"))
	if e != nil {
		return result, errors.New("center initialization busy; retry identical configuration")
	}
	defer unlock()
	var id Identity
	identityPath := filepath.Join(c.DataDir, "center-identity.json")
	raw, readErr := os.ReadFile(identityPath)
	if readErr == nil {
		if json.Unmarshal(raw, &id) != nil || id.ConfigDigest != Digest(c) || !samePath(id.DataDir, c.DataDir) {
			return result, errors.New("saved center data identity conflicts; preserved")
		}
	} else if !os.IsNotExist(readErr) {
		return result, readErr
	}
	if os.IsNotExist(readErr) {
		if _, e := os.Lstat(c.ChatProfile); e == nil {
			if _, ownerErr := os.Stat(filepath.Join(c.DataDir, "owner.token")); ownerErr != nil {
				return result, errors.New("existing limited profile cannot establish a new center identity; choose a new profile path")
			}
			if _, dbErr := os.Stat(filepath.Join(c.DataDir, "coordica.db")); dbErr != nil {
				return result, errors.New("existing limited profile has no matching saved center registration; choose a new profile path")
			}
		} else if !os.IsNotExist(e) {
			return result, e
		}
	}
	if result.MatchingCenterHealthy {
		// An already healthy instance owns its data lock. Provision only through
		// its authenticated API; the startup closure handles offline and online.
		result.Service.State = "already_running"
		if m.Provision != nil {
			if e = m.Provision(ctx, nil, c); e != nil {
				return result, e
			}
			ready := true
			result.ChatReady = &ready
		}
		return result, nil
	}
	// Check BOTH endpoints before credentials/TLS/DB/service mutation. A bare
	// healthz response never establishes ownership of either occupied port.
	reserve := m.ReservePorts
	if reserve == nil {
		reserve = reservePorts
	}
	releasePorts, e := reserve(ctx, []string{c.ConsoleListen, c.RunnerListen})
	if e != nil {
		return result, e
	}
	releasePorts()

	release, e := filelock.Acquire(filepath.Join(c.DataDir, "center.lock"))
	if e != nil {
		return result, errors.New("center data is owned by a live or unknown process; service untouched")
	}
	db, e := store.Open(filepath.Join(c.DataDir, "coordica.db"))
	if e != nil {
		release()
		return result, e
	}
	s := app.New(db, c.DataDir)
	if existing, err := s.Project(ctx); err == nil && (existing.RepositoryURL != c.RepositoryURL() || projectSource(existing) != c.Repository || existing.TargetBranch != c.Branch || existing.Name != c.Name) {
		db.Close()
		release()
		return result, errors.New("existing project identity differs; history preserved; choose a new center instance")
	} else if err != nil && !errors.Is(err, domain.ErrNotFound) {
		db.Close()
		release()
		return result, err
	}
	if os.IsNotExist(readErr) {
		if _, existsErr := os.Lstat(c.ChatProfile); existsErr == nil {
			profile, profileErr := chatprofile.Load(c.ChatProfile)
			if profileErr != nil {
				db.Close()
				release()
				return result, errors.New("existing limited profile invalid; preserved")
			}
			actor, authErr := s.Authenticate(ctx, profile.Token)
			if authErr != nil || profile.CenterURL != c.ConsoleURL() || actor.Role != "chat" || actor.ChatScope != domain.ChatScopeCenter || actor.NodeID != "" {
				db.Close()
				release()
				return result, errors.New("existing limited profile does not match saved center registration; preserved")
			}
		} else if !os.IsNotExist(existsErr) {
			db.Close()
			release()
			return result, existsErr
		}
	}

	_, e = s.Bootstrap(ctx)
	owner := app.Actor{ID: "owner", Role: "owner"}
	if e == nil {
		p, pe := s.Project(ctx)
		if errors.Is(pe, domain.ErrNotFound) {
			_, e = s.CreateDraftProject(ctx, owner, "center-host-project", domain.Project{Name: c.Name, RepositoryURL: c.RepositoryURL(), SourceRepository: c.Repository, TargetBranch: c.Branch, Checks: c.Checks})
		} else if pe != nil {
			e = pe
		} else if p.RepositoryURL != c.RepositoryURL() || projectSource(p) != c.Repository || p.TargetBranch != c.Branch || p.Name != c.Name {
			e = errors.New("existing project identity differs; history preserved; select a new center data instance")
		}
	}
	if e == nil && readErr != nil {
		id = Identity{InstanceID: app.ID("center_"), ConfigDigest: Digest(c), DataDir: c.DataDir}
		raw, _ = json.Marshal(id)
		e = localfile.WritePrivate(identityPath, raw)
	}
	if e == nil {
		e = db.Update(ctx, func(tx *store.Tx) error {
			var saved Identity
			e := tx.Get("center_identity", "current", &saved)
			if e == nil && saved != id {
				return errors.New("database center identity conflicts")
			}
			if e != nil && !errors.Is(e, domain.ErrNotFound) {
				return e
			}
			return tx.Put("center_identity", "current", id)
		})
	}
	if e == nil && c.TLS() {
		e = ensureTLS(c)
	}
	if e == nil && m.Provision != nil {
		e = m.Provision(ctx, s, c)
		ready := e == nil
		result.ChatReady = &ready
	}
	if e == nil {
		p, err := s.Project(ctx)
		if err != nil {
			e = err
		} else {
			result.ProjectPaused = &p.Paused
		}
		settings := domain.DefaultAutomationSettings()
		if e == nil {
			snap, err := s.Snapshot(ctx, owner)
			if err != nil {
				e = err
			} else {
				settings = snap.Automation
			}
		}
		result.AutomationEnabled = &settings.Enabled
	}
	db.Close()
	release()
	if e != nil {
		return result, e
	}
	result.Initialized = true
	if e = saveOperation(c, nodeservice.Status{State: "requesting_activation"}); e != nil {
		return result, e
	}
	installer := m.Installer
	if installer == nil {
		installer = nodeservice.Manager{}
	}
	result.Service, e = installer.EnsureStarted(ctx, nodeservice.Spec{Executable: c.Executable, DataDir: c.DataDir, RuntimePath: c.RuntimePath, CenterConfig: m.ConfigPath, HTTPSProxy: c.HTTPSProxy})
	if e != nil {
		saveOperation(c, nodeservice.Status{State: "activation_failed"})
		return result, fmt.Errorf("center configuration retained; user service activation failed: %w", e)
	}
	if result.Service.State != "started" && result.Service.State != "activation_requested" {
		return result, errors.New("user service manager did not confirm an activation request")
	}
	if e = saveOperation(c, result.Service); e != nil {
		return result, e
	}
	actual, statusErr := m.Status(ctx)
	if statusErr == nil && actual.MatchingCenterHealthy {
		actual.Service = result.Service
		result = actual
	}
	if !result.MatchingCenterHealthy {
		result.Detail = "OS activation requested; matching authenticated center health is not yet confirmed; use get_host_status"
	} else {
		result.Detail = "matching center healthy"
	}
	return result, nil
}

func (m Manager) runtime(ctx context.Context, c Config, expected Identity) (domain.CenterRuntimeStatus, error) {
	var out domain.CenterRuntimeStatus
	raw, e := os.ReadFile(filepath.Join(c.DataDir, "owner.token"))
	if e != nil {
		return out, e
	}
	api, e := apiclient.New(c.ConsoleURL(), strings.TrimSpace(string(raw)), "")
	if e != nil {
		return out, e
	}
	defer api.CloseIdleConnections()
	call, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	data, e := api.Do(call, "GET", "/api/v1/system/host-status", "", nil)
	if e != nil {
		return out, e
	}
	if json.Unmarshal(data, &out) != nil || out.Identity != expected {
		return out, errors.New("matching center identity unavailable")
	}
	return out, nil
}
func (m Manager) verifyProfile(ctx context.Context, c Config) error {
	p, e := chatprofile.Load(c.ChatProfile)
	if e != nil {
		return e
	}
	if p.CenterURL != c.ConsoleURL() || p.TLSCA != "" {
		return errors.New("profile center conflict")
	}
	api, e := apiclient.New(p.CenterURL, p.Token, p.TLSCA)
	if e != nil {
		return e
	}
	defer api.CloseIdleConnections()
	call, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	raw, e := api.Do(call, "GET", "/api/v1/chat/identity", "", nil)
	if e != nil {
		return e
	}
	var id domain.ChatCredentialInfo
	if json.Unmarshal(raw, &id) != nil || id.ID != p.CredentialID || id.ChatScope != domain.ChatScopeCenter || id.NodeID != "" || id.Revoked {
		return errors.New("center limited profile inactive or conflicting")
	}
	return nil
}

func saveOperation(c Config, service nodeservice.Status) error {
	raw, e := json.Marshal(struct {
		Digest  string             `json:"digest"`
		Service nodeservice.Status `json:"service"`
	}{Digest(c), service})
	if e != nil {
		return e
	}
	return localfile.WritePrivate(filepath.Join(c.DataDir, "center-operation.json"), raw)
}

func (m Manager) runnerHealth(ctx context.Context, c Config, id Identity) error {
	if c.RunnerListen == c.ConsoleListen {
		return nil
	}
	host, port, e := net.SplitHostPort(c.RunnerListen)
	if e != nil {
		return e
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		host = "127.0.0.1"
	}
	api, e := apiclient.New("https://"+net.JoinHostPort(host, port), "", filepath.Join(c.DataDir, "center.crt"))
	if e != nil {
		return e
	}
	defer api.CloseIdleConnections()
	call, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	health, e := api.PublicHealth(call)
	if e != nil {
		return e
	}
	if health.CenterInstanceID != id.InstanceID || health.ConfigDigest != id.ConfigDigest {
		return errors.New("Runner endpoint belongs to another instance")
	}
	return nil
}

func projectSource(p domain.Project) string {
	if p.SourceRepository != "" {
		return p.SourceRepository
	}
	return p.RepositoryURL
}
