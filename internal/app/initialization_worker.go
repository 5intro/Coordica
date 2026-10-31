package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/localfile"
	"coordica/internal/process"
	"coordica/internal/store"
)

type InitializationWorker struct {
	Service  *Service
	Executor PlanningExecutor
}
type initializationReceipt struct {
	CheckInputs    []string                `json:"check_inputs,omitempty"`
	OperationID    string                  `json:"operation_id"`
	Version        int64                   `json:"version"`
	Generation     int64                   `json:"generation"`
	Outcome        PlanningOutcome         `json:"outcome"`
	Snapshot       gitops.AnalysisSnapshot `json:"snapshot"`
	GitTreeUnknown bool                    `json:"git_tree_unknown"`
	InputUnchanged bool                    `json:"input_unchanged"`
}

func (w *InitializationWorker) receipt(id string) string {
	return filepath.Join(w.Service.Dir, "initialization-outcomes", id+".json")
}
func initializationBusy(tx *store.Tx) (bool, error) {
	ops, e := store.List[domain.InitializationOperation](tx, "initialization_operation")
	if e != nil {
		return false, e
	}
	for _, op := range ops {
		if !op.Settled {
			return true, nil
		}
	}
	return false, nil
}
func (w *InitializationWorker) Tick(ctx context.Context) error {
	s := w.Service
	var op domain.InitializationOperation
	var v domain.Initialization
	var p domain.Project
	e := s.DB.Update(ctx, func(tx *store.Tx) error {
		var e error
		v, e = initialization(tx)
		if e != nil || v.OperationID == "" {
			return e
		}
		if e = tx.Get("initialization_operation", v.OperationID, &op); e != nil {
			return e
		}
		if op.State != "queued" {
			op.ID = ""
			return nil
		}
		if e = initializationIssuer(tx, op); e != nil {
			op.State = "rejected"
			op.Settled = true
			v.Status = "draft"
			v.Detail = "initiating center credential revoked or rebound"
			if e = tx.Put("initialization_operation", op.ID, op); e != nil {
				return e
			}
			if e = tx.Put("initialization", "current", v); e != nil {
				return e
			}
			op.ID = ""
			return s.audit(tx, Actor{ID: "center"}, "initialization.rejected", v.OperationID, "issuer invalid")
		}
		if e = initializationQuiet(tx); e != nil {
			op.ID = ""
			return nil
		}
		p, e = getProject(tx)
		if e != nil {
			return e
		}
		if op.InitializationVersion != v.Version || initializationPolicy(p) != v.PolicyDigest {
			return domain.Fail("version_conflict", "初始化输入已变化")
		}
		op.State = "running"
		op.Version++
		op.Attempt++
		op.Deadline = s.Now().Add(time.Duration(op.GrantSeconds) * time.Second)
		p.ReservedSeconds += op.GrantSeconds
		p.Version++
		if e = tx.Put("project", p.ID, p); e != nil {
			return e
		}
		if e = tx.Put("initialization_operation", op.ID, op); e != nil {
			return e
		}
		return s.audit(tx, Actor{ID: "center"}, "initialization.started", op.ID, fmt.Sprint(op.Generation))
	})
	if e != nil || op.ID == "" {
		return e
	}
	callCtx, cancel := context.WithDeadline(ctx, op.Deadline)
	defer cancel()
	unregister := s.registerInitializationCancel(op.ID, cancel)
	defer unregister()
	s.cancelInvalidInitializations(callCtx)
	r := initializationReceipt{OperationID: op.ID, Version: op.InitializationVersion, Generation: op.Generation, Outcome: PlanningOutcome{TreeStopped: true}, InputUnchanged: true}
	started := time.Now()
	if v.Kind == "existing" {
		r.Snapshot, e = s.analysisSource().ReadAnalysis(callCtx, analysisRepository(p), v.SourceSHA)
		r.GitTreeUnknown = errors.Is(e, process.ErrTreeNotStopped)
		for _, file := range r.Snapshot.Files {
			v.AvailablePaths = append(v.AvailablePaths, file.Path)
		}
		v.Omissions = r.Snapshot.Omissions
	}
	if e == nil && v.Kind == "existing" {
		r.CheckInputs, e = readCheckInputs(callCtx, s, p, v.SourceSHA)
		v.CheckInputPaths = r.CheckInputs
	}
	var dir string
	if e == nil {
		dir, e = os.MkdirTemp("", "coordica-initialization-")
	}
	if e == nil {
		e = callCtx.Err()
	}
	if e == nil {
		e = materializeAnalysis(dir, r.Snapshot)
	}
	if e == nil {
		e = s.DB.Update(ctx, func(tx *store.Tx) error {
			var current domain.InitializationOperation
			if e := tx.Get("initialization_operation", op.ID, &current); e != nil {
				return e
			}
			if current.State != "running" || current.Generation != op.Generation {
				return domain.Fail("stale_run", "初始化已取消")
			}
			current.WorkingDirectory = dir
			return tx.Put("initialization_operation", current.ID, current)
		})
	}
	if e == nil && w.Executor == nil {
		e = errors.New("initialization executor unavailable")
	}
	if e == nil {
		r.Outcome, e = executeInitializationLeader(callCtx, w, op, p, v, dir)
	}
	if e != nil {
		r.Outcome.Error = planningErrorText(e)
		if errors.Is(e, process.ErrTreeNotStopped) {
			r.Outcome.TreeStopped = false
		}
	}
	if r.Outcome.ElapsedSeconds < int64(time.Since(started)/time.Second)+1 {
		r.Outcome.ElapsedSeconds = int64(time.Since(started)/time.Second) + 1
	}
	reconcileCtx, reconcileCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	sha, reconcileErr := s.analysisSource().AnalysisRevision(reconcileCtx, analysisRepository(p), p.TargetBranch)
	reconcileCancel()
	r.InputUnchanged = v.Kind == "existing" && reconcileErr == nil && sha == v.SourceSHA || v.Kind == "new" && errors.Is(reconcileErr, gitops.ErrUnborn)
	if errors.Is(reconcileErr, process.ErrTreeNotStopped) {
		r.Outcome.TreeStopped = false
		r.GitTreeUnknown = true
	}
	if reconcileErr != nil && !errors.Is(reconcileErr, gitops.ErrUnborn) {
		r.Outcome.Error = planningErrorText(reconcileErr)
	}
	r.Outcome.ElapsedSeconds = max(r.Outcome.ElapsedSeconds, int64(time.Since(started)/time.Second)+1)
	for i := range r.Snapshot.Files {
		r.Snapshot.Files[i].Text = ""
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if err = localfile.WritePrivate(w.receipt(op.ID), raw); err != nil {
		return err
	}
	err = w.apply(context.WithoutCancel(ctx), r)
	if r.Outcome.TreeStopped && dir != "" {
		removeAnalysis(dir)
	}
	return err
}
func planningErrorText(e error) string {
	text := e.Error()
	if len(text) > 4096 {
		text = text[:4096]
	}
	return text
}
func (w *InitializationWorker) apply(ctx context.Context, r initializationReceipt) error {
	s := w.Service
	return s.DB.Update(ctx, func(tx *store.Tx) error {
		var op domain.InitializationOperation
		if e := tx.Get("initialization_operation", r.OperationID, &op); e != nil {
			return e
		}
		if op.Settled || op.Generation != r.Generation || op.InitializationVersion != r.Version {
			return nil
		}
		v, e := initialization(tx)
		if e != nil {
			return e
		}
		p, e := getProject(tx)
		if e != nil {
			return e
		}
		if !r.Outcome.TreeStopped {
			if r.GitTreeUnknown {
				op.ProcessID = 0
			}
			op.State = "unknown_live"
			v.Detail = "初始化进程停止未确认；保留预留支出，需 Owner 核验停止"
			v.Status = "analyzing"
		} else {
			op.TreeStopped = true
			op.Settled = true
			op.ElapsedSeconds = r.Outcome.ElapsedSeconds
			if op.ElapsedSeconds < 0 {
				return domain.Fail("invalid_input", "negative initialization accounting")
			}
			op.UsedSeconds += op.ElapsedSeconds
			p.ReservedSeconds -= op.GrantSeconds
			p.UsedSeconds += op.ElapsedSeconds
			p.Version++
			v.AvailablePaths = []string{}
			for _, file := range r.Snapshot.Files {
				v.AvailablePaths = append(v.AvailablePaths, file.Path)
			}
			v.Omissions = r.Snapshot.Omissions
			v.CheckInputPaths = r.CheckInputs
			result, err := decodeInitialization(r.Outcome.Output, v)
			cancelled := op.State == "cancel_requested"
			valid := initializationIssuer(tx, op) == nil && err == nil && r.Outcome.Error == "" && r.InputUnchanged && v.OperationID == op.ID && v.Version == r.Version && p.Paused && initializationPolicy(p) == v.PolicyDigest && s.Now().Before(op.Deadline) && op.State != "cancel_requested"
			if valid {
				v.CoveredPaths = result.InspectedPaths
				for _, path := range v.AvailablePaths {
					found := false
					for _, read := range result.InspectedPaths {
						if path == read {
							found = true
						}
					}
					if !found {
						v.Omissions = append(v.Omissions, path+": available but not reported inspected")
					}
				}
				v.Result = &result
				v.Detail = ""
				v.Status = "interview"
				if result.Ready {
					v.Status = "awaiting_owner"
				}
				v.Digest = initializationDigest(v)
				op.State = "completed"
			} else {
				v.Status = "draft"
				v.Detail = "Leader 结果无效、超时、输入变化或已取消；未授予项目权限"
				op.State = "failed"
				// Only a stopped transient model failure may retry, bounded to three
				// attempts (900 seconds reserved maximum); malformed/replayed input
				// or changed policy never auto-admits or blindly repeats questions.
				if r.Outcome.Error != "" && r.InputUnchanged && !cancelled && initializationIssuer(tx, op) == nil && op.Attempt <= op.MaxRetries && initializationPolicy(p) == v.PolicyDigest && v.Version == r.Version {
					op.State = "queued"
					op.Generation++
					op.Settled = false
					op.TreeStopped = false
					op.ProcessID = 0
					op.WorkingDirectory = ""
					v.Status = "analyzing"
				}
			}
		}
		op.Version++
		if e = tx.Put("initialization_operation", op.ID, op); e != nil {
			return e
		}
		if e = tx.Put("project", p.ID, p); e != nil {
			return e
		}
		if e = tx.Put("initialization", "current", v); e != nil {
			return e
		}
		return s.audit(tx, Actor{ID: "center"}, "initialization.result", op.ID, op.State)
	})
}
func (w *InitializationWorker) Recover(ctx context.Context) error {
	var ops []domain.InitializationOperation
	if e := w.Service.DB.View(ctx, func(tx *store.Tx) error {
		var e error
		ops, e = store.List[domain.InitializationOperation](tx, "initialization_operation")
		return e
	}); e != nil {
		return e
	}
	for _, op := range ops {
		if op.Settled || op.State == "queued" {
			continue
		}
		raw, e := os.ReadFile(w.receipt(op.ID))
		var receipt initializationReceipt
		if e == nil && len(raw) <= 3<<20 && json.Unmarshal(raw, &receipt) == nil && receipt.OperationID == op.ID && receipt.Version == op.InitializationVersion && receipt.Generation == op.Generation {
			if e = w.apply(ctx, receipt); e != nil {
				return e
			}
			continue
		}
		if e = w.Service.DB.Update(ctx, func(tx *store.Tx) error {
			var current domain.InitializationOperation
			if e := tx.Get("initialization_operation", op.ID, &current); e != nil {
				return e
			}
			if current.Settled {
				return nil
			}
			current.State = "unknown_live"
			current.Version++
			if e := tx.Put("initialization_operation", op.ID, current); e != nil {
				return e
			}
			v, e := initialization(tx)
			if e != nil {
				return e
			}
			v.Detail = "重启后没有匹配停止回执；请 Owner 核验停止，不能重新分析或执行"
			if e = tx.Put("initialization", "current", v); e != nil {
				return e
			}
			return w.Service.audit(tx, Actor{ID: "center"}, "initialization.unknown_live", op.ID, "")
		}); e != nil {
			return e
		}
	}
	return nil
}

func materializeAnalysis(dir string, snapshot gitops.AnalysisSnapshot) error {
	if e := localfile.Protect(dir); e != nil {
		return e
	}
	seen := map[string]bool{}
	manifest := struct {
		SHA       string   `json:"sha"`
		Paths     []string `json:"paths"`
		Omissions []string `json:"omissions"`
	}{SHA: snapshot.SHA, Paths: []string{}, Omissions: snapshot.Omissions}
	for _, f := range snapshot.Files {
		key := strings.ToLower(f.Path)
		if seen[key] {
			return errors.New("case-ambiguous source paths cannot be materialized")
		}
		seen[key] = true
		path := filepath.Join(dir, "source", filepath.FromSlash(f.Path))
		relative, e := filepath.Rel(filepath.Join(dir, "source"), path)
		if e != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return errors.New("unsafe snapshot path")
		}
		if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			return e
		}
		if e = os.WriteFile(path, []byte(f.Text), 0400); e != nil {
			return e
		}
		manifest.Paths = append(manifest.Paths, f.Path)
	}
	raw, e := json.MarshalIndent(manifest, "", "  ")
	if e != nil {
		return e
	}
	if len(raw) > 1<<20 {
		return errors.New("analysis manifest exceeds limit")
	}
	return os.WriteFile(filepath.Join(dir, "source-manifest.json"), raw, 0400)
}
func removeAnalysis(dir string) { os.RemoveAll(dir) }
