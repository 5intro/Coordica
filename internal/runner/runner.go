// Package runner owns local execution, leases and durable handoffs. Authority
// and integration remain with the center's versioned API.
package runner

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"coordica/internal/agent"
	"coordica/internal/domain"
	"coordica/internal/filelock"
	"coordica/internal/gitops"
	"coordica/internal/localfile"
)

type Config struct {
	HTTPSProxy    string             `json:"https_proxy,omitempty"`
	CenterURL     string             `json:"center_url"`
	DataDir       string             `json:"data_dir"`
	Name          string             `json:"name"`
	TLSCA         string             `json:"tls_ca"`
	RuntimePath   string             `json:"runtime_path,omitempty"`
	ConsoleListen string             `json:"console_listen,omitempty"`
	NodeID        string             `json:"node_id"`
	Token         string             `json:"token"`
	Capacity      int                `json:"capacity"`
	Executables   map[string]string  `json:"executables,omitempty"`
	Tools         []agent.Definition `json:"tools,omitempty"`
	PollInterval  time.Duration      `json:"poll_interval,omitempty"`
}
type Runner struct {
	// OnEvent may be set before Run to connect diagnostics to a local UI.
	// The default emits concise local stderr logs; callbacks must return promptly.
	OnEvent         func(Event)
	config          Config
	http            *http.Client
	base            *url.URL
	git             *gitops.Client
	registry        *agent.Registry
	execute         func(context.Context, agent.Request, func(agent.Event)) (agent.Result, error)
	readReviewInput func(context.Context, domain.Assignment, string) (string, error)
	mu              sync.Mutex
	active          map[string]bool
	logMu           sync.Mutex
	lastLog         map[string]string
}

type Event struct {
	Time   time.Time `json:"time"`
	Kind   string    `json:"kind"`
	RunID  string    `json:"run_id,omitempty"`
	Detail string    `json:"detail"`
}

func (r *Runner) report(kind, runID, detail string) {
	detail = r.safeText(detail)
	r.logMu.Lock()
	defer r.logMu.Unlock()
	key := kind + ":" + runID
	if r.lastLog[key] == detail {
		return
	}
	r.lastLog[key] = detail
	event := Event{Time: time.Now().UTC(), Kind: kind, RunID: runID, Detail: detail}
	if r.OnEvent != nil {
		r.OnEvent(event)
	} else {
		log.Printf("runner %s run=%s %s", kind, runID, detail)
	}
}

func New(config Config) (*Runner, error) {
	if err := ValidateHTTPSProxy(config.HTTPSProxy); err != nil {
		return nil, err
	}
	if config.DataDir == "" {
		return nil, errors.New("runner data directory is required")
	}
	var err error
	config.DataDir, err = filepath.Abs(config.DataDir)
	if err != nil {
		return nil, err
	}
	if config.Capacity == 0 {
		config.Capacity = 2
	}
	if config.Capacity < 1 || config.Capacity > 2 {
		return nil, errors.New("Runner capacity must be 1 or 2")
	}
	if config.Name == "" {
		config.Name, _ = os.Hostname()
	}
	if config.PollInterval <= 0 {
		config.PollInterval = time.Second
	}
	if config.Executables == nil {
		config.Executables = map[string]string{}
	}
	registry, err := LocalRegistry(config.Tools)
	if err != nil {
		return nil, err
	}
	config.Executables, err = NormalizeExecutables(registry, config.Executables)
	if err != nil {
		return nil, err
	}
	client, base, err := newHTTPClient(config)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Join(config.DataDir, "spool"), 0700); err != nil {
		return nil, err
	}
	if err = localfile.Protect(config.DataDir); err != nil {
		return nil, err
	}
	if err = localfile.Protect(filepath.Join(config.DataDir, "spool")); err != nil {
		return nil, err
	}
	r := &Runner{config: config, http: client, base: base, git: gitops.New("git"), registry: registry, execute: registry.Execute, active: map[string]bool{}, lastLog: map[string]string{}}
	r.readReviewInput = r.reviewInput
	return r, nil
}
func (r *Runner) Config() Config { return r.config }
func (r *Runner) capabilities(ctx context.Context) []domain.Capability {
	var result []domain.Capability
	for _, cap := range r.registry.Discover(ctx, r.config.Executables) {
		tool := cap.Tool
		r.report("capability", tool, fmt.Sprintf("available=%t version=%s verified=%t %s; %s", cap.Available, cap.Version, cap.Verified, cap.Detail, cap.Approvals))
		result = append(result, domain.Capability{Tool: cap.Tool, Version: cap.Version, Installed: cap.Installed, Available: cap.Available, Structured: cap.Structured, Protocol: cap.Protocol, Review: cap.Review, ReadOnly: cap.ReadOnly, Resume: cap.Resume, Cancel: cap.Cancel, Detail: cap.Detail + "; " + cap.Approvals})
		result[len(result)-1].ModelDiscovery = cap.ModelDiscovery
		for _, model := range cap.Models {
			result[len(result)-1].Models = append(result[len(result)-1].Models, domain.ModelOption{ID: model.ID, Name: model.Name, ReasoningEfforts: append([]string{}, model.ReasoningEfforts...), DefaultReasoningEffort: model.DefaultReasoningEffort})
		}
	}
	return result
}
func (r *Runner) Register(ctx context.Context, code string) error {
	unlock, err := filelock.Acquire(filepath.Join(r.config.DataDir, "runner.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	request := domain.RegisterRequest{Code: code, Name: r.config.Name, Platform: runtime.GOOS + "/" + runtime.GOARCH, Capacity: r.config.Capacity, Capabilities: r.capabilities(ctx)}
	var response domain.RegisterResponse
	if _, err = r.json(ctx, "POST", "/api/v1/runner/register", "", request, &response); err != nil {
		return err
	}
	if response.NodeID == "" || response.Token == "" {
		return errors.New("center returned incomplete node identity")
	}
	r.config.NodeID, r.config.Token = response.NodeID, response.Token
	return writePrivateJSON(filepath.Join(r.config.DataDir, "runner.json"), r.config)
}

// Run resumes pending handoffs before accepting work, and waits for local
// workers to stop on cancellation. Closing the browser has no effect on it.
func (r *Runner) Run(ctx context.Context) error {
	if r.config.NodeID == "" || r.config.Token == "" {
		return errors.New("Runner must be paired first")
	}
	unlock, err := filelock.Acquire(filepath.Join(r.config.DataDir, "runner.lock"))
	if err != nil {
		return fmt.Errorf("Runner directory is already in use: %w", err)
	}
	defer unlock()
	if err = r.recoverRecords(ctx); err != nil {
		r.report("recovery.failed", "", shortDetail(err))
		return err
	}
	var workers sync.WaitGroup
	ctx, cancel := context.WithCancel(ctx)
	defer func() { cancel(); workers.Wait() }()
	for {
		if ctx.Err() != nil {
			return nil
		}
		r.flush(ctx)
		count, err := r.occupiedSlots()
		if err != nil {
			r.report("spool.capacity_failed", "", shortDetail(err))
			return err
		}
		// Presence describes the Runner, not a free execution slot. Uncertain
		// recovery records retain capacity without hiding this live process.
		if err := r.presence(ctx); err != nil {
			r.report("presence.failed", "", shortDetail(err))
			var remote *apiError
			if errors.As(err, &remote) && (remote.Status == 401 || remote.Status == 403) {
				return err
			}
		} else if count < r.config.Capacity {
			assignment, clock, err := r.claim(ctx)
			if err != nil {
				r.report("claim.failed", "", shortDetail(err))
				var remote *apiError
				if errors.As(err, &remote) && (remote.Status == 401 || remote.Status == 403) {
					return err
				}
			}
			if err == nil && assignment != nil {
				if assignment.Run.ID == "" || assignment.Run.Generation <= 0 || assignment.Run.NodeID != r.config.NodeID {
					return errors.New("invalid or foreign assignment")
				}
				if assignment.Run.Kind != "implement" && assignment.Run.Kind != "review" {
					return errors.New("unsupported assignment role")
				}
				if _, e := os.Stat(r.spoolPath(assignment.Run.ID)); e == nil {
					os.Remove(filepath.Join(r.config.DataDir, "claim.json"))
					continue
				} else if !errors.Is(e, os.ErrNotExist) {
					return e
				}
				record := spool{Assignment: *assignment, Key: randomKey(), StartedAt: time.Now(), WorkDir: filepath.Join(r.config.DataDir, "runs", runName(assignment.Run.ID), "工作区")}
				if err = writePrivateJSON(r.spoolPath(assignment.Run.ID), record); err != nil {
					return err
				}
				os.Remove(filepath.Join(r.config.DataDir, "claim.json"))
				r.mu.Lock()
				already := r.active[assignment.Run.ID]
				r.active[assignment.Run.ID] = true
				r.mu.Unlock()
				if !already {
					r.report("run.assigned", assignment.Run.ID, assignment.Run.Kind+"/"+assignment.Run.Tool)
					workers.Add(1)
					go func() {
						defer workers.Done()
						defer func() { r.mu.Lock(); delete(r.active, record.Assignment.Run.ID); r.mu.Unlock() }()
						r.perform(ctx, record, clock)
					}()
				}
			} else if err == nil {
				os.Remove(filepath.Join(r.config.DataDir, "claim.json"))
			}
		}
		timer := time.NewTimer(r.config.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func (r *Runner) pendingClaim(ctx context.Context) (claimPending, error) {
	path := filepath.Join(r.config.DataDir, "claim.json")
	var pending claimPending
	if err := readJSON(path, &pending); errors.Is(err, os.ErrNotExist) {
		pending = claimPending{Key: randomKey(), Request: domain.ClaimRequest{Capabilities: r.capabilities(ctx)}}
		if err := writePrivateJSON(path, pending); err != nil {
			return claimPending{}, err
		}
	} else if err != nil {
		return claimPending{}, err
	}
	return pending, nil
}

func (r *Runner) presence(ctx context.Context) error {
	pending, err := r.pendingClaim(ctx)
	if err != nil {
		return err
	}
	// Presence is independent of the baseline/goal gates on Claim. Reuse the
	// persisted capability report so retries do not spawn discovery processes
	// or change the body/key of a potentially acknowledged work claim.
	_, err = r.json(ctx, "POST", "/api/v1/runner/presence", "", pending.Request, nil)
	return err
}

func (r *Runner) claim(ctx context.Context) (*domain.Assignment, responseClock, error) {
	pending, err := r.pendingClaim(ctx)
	if err != nil {
		return nil, responseClock{}, err
	}
	var assignment *domain.Assignment
	clock, err := r.json(ctx, "POST", "/api/v1/runner/claim", pending.Key, pending.Request, &assignment)
	return assignment, clock, err
}

func (r *Runner) recoverRecords(ctx context.Context) error {
	entries, err := os.ReadDir(filepath.Join(r.config.DataDir, "spool"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(r.config.DataDir, "spool", entry.Name())
		var record spool
		if err := readJSON(path, &record); err != nil {
			return fmt.Errorf("cannot recover handoff %s: %w", entry.Name(), err)
		}
		if record.Intent != nil {
			// This receipt already contains an actual stopped-tree observation.
			// Replay it before Complete rather than replacing it with PID inference.
			continue
		}
		if record.Request != nil {
			if record.Acknowledged && record.Request.Quiescent {
				// A durable local stop observation remains valid even if the
				// center lost its acknowledgement during restore. Replay the
				// original completion; do not downgrade it from a PID check.
				record.Acknowledged = false
				if err := writePrivateJSON(path, record); err != nil {
					return err
				}
			}
			if !record.Acknowledged {
				continue
			}
		}
		var remote domain.Run
		if _, err := r.json(ctx, "GET", "/api/v1/runs/"+record.Assignment.Run.ID, "", nil, &remote); err != nil {
			return fmt.Errorf("cannot reconcile previous run: %w", err)
		}
		if remote.Quiescent && (remote.State == "succeeded" || remote.State == "failed" || remote.State == "stopped") {
			os.Remove(path)
			continue
		}
		quiet, err := recordedTreeQuiescent(record.PID)
		if err != nil {
			quiet = false
		}
		request := domain.CompleteRequest{Generation: record.Assignment.Run.Generation, Status: "stopped", Detail: "Runner restarted; preserved workspace and reconciled prior process", Quiescent: quiet}
		if !quiet {
			request.Detail = "Runner restarted; prior process quiescence cannot be confirmed"
			r.report("recovery.process_unknown", record.Assignment.Run.ID, request.Detail)
		}
		record.Key = randomKey()
		record.Request = &request
		record.Acknowledged = false
		if err := writePrivateJSON(path, record); err != nil {
			return err
		}
	}
	r.flush(ctx)
	return nil
}

// A crashed run with unknown quiescence still consumes a local execution
// slot. Reporting it terminal to the center does not make a possibly live
// process disappear, and must not permit a third local agent.
func (r *Runner) occupiedSlots() (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := len(r.active)
	entries, err := os.ReadDir(filepath.Join(r.config.DataDir, "spool"))
	if err != nil {
		return count, err
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var record spool
		if err := readJSON(filepath.Join(r.config.DataDir, "spool", entry.Name()), &record); err != nil {
			return count, err
		}
		if !r.active[record.Assignment.Run.ID] && ((record.Request != nil && !record.Request.Quiescent) || (record.Request == nil && record.Intent != nil && !record.Intent.Completion.Quiescent)) {
			count++
		}
	}
	return count, nil
}

func (r *Runner) flush(ctx context.Context) {
	entries, err := os.ReadDir(filepath.Join(r.config.DataDir, "spool"))
	if err != nil {
		r.report("spool.read_failed", "", shortDetail(err))
		return
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var record spool
		path := filepath.Join(r.config.DataDir, "spool", entry.Name())
		if err := readJSON(path, &record); err != nil {
			r.report("spool.invalid", entry.Name(), shortDetail(err))
			continue
		}
		if record.Request == nil && record.Intent == nil {
			continue
		}
		r.mu.Lock()
		active := r.active[record.Assignment.Run.ID]
		r.mu.Unlock()
		if active {
			continue
		}
		if record.Acknowledged {
			if record.Request.Quiescent {
				continue
			}
			var confirmed bool
			record, confirmed, err = r.recheckStopped(path, record)
			if err != nil {
				r.report("recovery.process_unknown", record.Assignment.Run.ID, shortDetail(err))
				continue
			}
			if !confirmed {
				continue
			}
			r.report("recovery.process_stopped", record.Assignment.Run.ID, "recorded process group is now stopped; preserved workspace")
		}
		if err := r.deliver(ctx, record); err != nil {
			r.report("handoff.pending", record.Assignment.Run.ID, shortDetail(err))
		}
	}
}
func (r *Runner) deliver(ctx context.Context, record spool) error {
	if record.Intent != nil && record.Request == nil {
		if err := r.attemptIntent(ctx, &record); err != nil {
			return err
		}
		if !record.Intent.Settled {
			return errors.New("native intent receipt still pending")
		}
		record.Request = &record.Intent.Completion
		if err := writePrivateJSON(r.spoolPath(record.Assignment.Run.ID), record); err != nil {
			return err
		}
	}
	if record.Request == nil {
		return errors.New("handoff has no fixed completion request")
	}
	var remote domain.Run
	_, err := r.json(ctx, "POST", "/api/v1/runs/"+record.Assignment.Run.ID+"/complete", record.Key, record.Request, &remote)
	if err != nil {
		return err
	}
	if remote.ID != record.Assignment.Run.ID || remote.Generation != record.Assignment.Run.Generation {
		return errors.New("completion acknowledgement names another Run")
	}
	if record.Request.Quiescent {
		r.report("handoff.acknowledged", record.Assignment.Run.ID, "state="+remote.State)
		return os.Remove(r.spoolPath(record.Assignment.Run.ID))
	}
	record.Acknowledged = true
	return writePrivateJSON(r.spoolPath(record.Assignment.Run.ID), record)
}
