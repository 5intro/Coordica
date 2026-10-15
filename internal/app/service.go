// Package app owns all authorization and state transitions. Transports are thin
// callers; background work uses the same transaction rules as user actions.
package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/localfile"
	"coordica/internal/store"
)

type Actor struct{ ID, Role, NodeID, ChatScope string }
type Service struct {
	Source      AnalysisSource
	initMu      sync.Mutex
	initCancels map[string]context.CancelFunc
	DB          *store.Store
	Dir         string
	Now         func() time.Time
	Lease       time.Duration
}

func New(db *store.Store, dir string) *Service {
	return &Service{DB: db, Dir: dir, Now: func() time.Time { return time.Now().UTC() }, Lease: 45 * time.Second}
}
func ID(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(b)
}
func Hash(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
func owner(a Actor) error {
	if a.Role != "owner" {
		return domain.Fail("forbidden", "需要用户权限")
	}
	return nil
}
func version(got, want int64) error {
	if got != want {
		return domain.Fail("version_conflict", "对象版本已变化，请刷新后重试")
	}
	return nil
}
func (s *Service) audit(tx *store.Tx, a Actor, kind, id, detail string) error {
	return tx.Audit(domain.Event{Time: s.Now(), Actor: a.ID, Kind: kind, ObjectID: id, Detail: detail})
}
func (s *Service) mutate(ctx context.Context, a Actor, key, action string, input any, fn func(*store.Tx) (any, error)) (json.RawMessage, error) {
	return s.mutateWithCheck(ctx, a, key, action, input, nil, fn)
}
func (s *Service) mutateWithCheck(ctx context.Context, a Actor, key, action string, input any, check func(*store.Tx) error, fn func(*store.Tx) (any, error)) (json.RawMessage, error) {
	if key == "" || len(key) > 200 {
		return nil, domain.Fail("invalid_input", "写操作需要 Idempotency-Key")
	}
	b, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	digest := Hash(action + "\x00" + string(b))
	var result json.RawMessage
	err = s.DB.Update(ctx, func(tx *store.Tx) error {
		if check != nil {
			if err := check(tx); err != nil {
				return err
			}
		}
		replay, ok, e := tx.Replay(a.ID, key, digest)
		if e != nil {
			return e
		}
		if ok {
			result = replay
			return nil
		}
		value, e := fn(tx)
		if e != nil {
			return e
		}
		result, e = json.Marshal(value)
		if e != nil {
			return e
		}
		return tx.Remember(a.ID, key, digest, value)
	})
	if err == nil {
		s.cancelInvalidInitializations(ctx)
	}
	return result, err
}
func (s *Service) Bootstrap(ctx context.Context) (string, error) {
	path := filepath.Join(s.Dir, "owner.token")
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		token = ID("own_")
		if err = localfile.WritePrivate(path, []byte(token+"\n")); err != nil {
			return "", err
		}
	}
	if err = localfile.Protect(path); err != nil {
		return "", err
	}
	err = s.DB.Update(ctx, func(tx *store.Tx) error {
		var c domain.Credential
		e := tx.Get("credential", Hash(token), &c)
		if e == nil {
			return nil
		}
		if !errors.Is(e, domain.ErrNotFound) {
			return e
		}
		all, e := store.List[domain.Credential](tx, "credential")
		if e != nil {
			return e
		}
		for _, v := range all {
			if v.Role == "owner" {
				return errors.New("owner token does not match database; restore the original token")
			}
		}
		return tx.Put("credential", Hash(token), domain.Credential{ID: "owner", Hash: Hash(token), Role: "owner"})
	})
	if err == nil {
		err = s.MigrateAuthorityPolicy(ctx)
	}
	return path, err
}
func (s *Service) Authenticate(ctx context.Context, token string) (Actor, error) {
	var a Actor
	if token == "" {
		return a, domain.Fail("unauthorized", "缺少访问令牌")
	}
	err := s.DB.View(ctx, func(tx *store.Tx) error {
		var c domain.Credential
		if err := tx.Get("credential", Hash(token), &c); err != nil {
			return domain.Fail("unauthorized", "访问令牌无效")
		}
		if c.Revoked {
			return domain.Fail("unauthorized", "credential revoked")
		}
		if c.Role == "node" || c.Role == "chat" && c.ChatScope == domain.ChatScopeNode {
			var n domain.Node
			if err := tx.Get("node", c.NodeID, &n); err != nil || n.Revoked {
				return domain.Fail("unauthorized", "节点已撤销")
			}
		}
		if c.Role == "chat" && c.ChatScope != domain.ChatScopeCenter && c.ChatScope != domain.ChatScopeNode {
			return domain.Fail("unauthorized", "legacy unscoped chat credential requires explicit renewal")
		}
		a = Actor{ID: c.ID, Role: c.Role, NodeID: c.NodeID, ChatScope: c.ChatScope}
		return nil
	})
	return a, err
}
func (s *Service) Snapshot(ctx context.Context, a Actor) (domain.Snapshot, error) {
	return s.snapshot(ctx, a, 150)
}
func (s *Service) Export(ctx context.Context, a Actor) (domain.Snapshot, error) {
	return s.snapshot(ctx, a, -1)
}
func (s *Service) snapshot(ctx context.Context, a Actor, eventLimit int) (domain.Snapshot, error) {
	v := domain.Snapshot{APIVersion: domain.APIVersion}
	if err := owner(a); err != nil {
		return v, err
	}
	err := s.DB.View(ctx, func(tx *store.Tx) error {
		var p domain.Project
		err := tx.Get("project", "project", &p)
		if err == nil {
			v.Project = &p
		} else if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if v.Requirements, err = store.List[domain.Requirement](tx, "requirement"); err != nil {
			return err
		}
		if v.Tasks, err = store.List[domain.Task](tx, "task"); err != nil {
			return err
		}
		if v.Runs, err = store.List[domain.Run](tx, "run"); err != nil {
			return err
		}
		if v.Candidates, err = store.List[domain.Candidate](tx, "candidate"); err != nil {
			return err
		}
		if v.TestSuites, err = store.List[domain.TestSuite](tx, "test_suite"); err != nil {
			return err
		}
		if v.Nodes, err = store.List[domain.Node](tx, "node"); err != nil {
			return err
		}
		init, initErr := initialization(tx)
		if initErr != nil {
			return initErr
		}
		v.Initialization = &init
		if err = phaseBSnapshot(tx, &v); err != nil {
			return err
		}
		v.Events, err = tx.Events(eventLimit)
		return err
	})
	return v, err
}
func (s *Service) CreateProject(ctx context.Context, a Actor, key string, p domain.Project) (json.RawMessage, error) {
	return s.createProject(ctx, a, key, p, false)
}
func (s *Service) CreateDraftProject(ctx context.Context, a Actor, key string, p domain.Project) (json.RawMessage, error) {
	return s.createProject(ctx, a, key, p, true)
}
func (s *Service) createProject(ctx context.Context, a Actor, key string, p domain.Project, draft bool) (json.RawMessage, error) {
	if err := owner(a); err != nil {
		return nil, err
	}
	if p.TargetBranch == "" {
		p.TargetBranch = "main"
	}
	// Phase A dogfoods Coordica. Its authorization and integration controls
	// cannot accidentally become ordinary changes through an empty UI list.
	for _, required := range authorityPaths {
		found := false
		for _, configured := range p.ProtectedPaths {
			if configured == required {
				found = true
				break
			}
		}
		if !found {
			p.ProtectedPaths = append(p.ProtectedPaths, required)
		}
	}
	validation := p
	if draft && len(validation.Checks) == 0 {
		validation.Checks = []domain.Check{{Name: "draft validation only", Command: "git", TimeoutSeconds: 1}}
	}
	if err := domain.ValidateProject(validation); err != nil {
		return nil, err
	}
	if _, err := gitops.NewScope([]string{"**"}, p.ProtectedPaths); err != nil {
		return nil, domain.Fail("invalid_input", err.Error())
	}
	return s.mutate(ctx, a, key, "project.create", p, func(tx *store.Tx) (any, error) {
		var existing domain.Project
		if e := tx.Get("project", "project", &existing); e == nil {
			return nil, domain.Fail("conflict", "首版只允许一个项目")
		} else if !errors.Is(e, domain.ErrNotFound) {
			return nil, e
		}
		p.ID = "project"
		p.Version = 1
		p.PolicyVersion = 1
		p.Paused = true
		p.UsedSeconds = 0
		p.ReservedSeconds = 0
		p.CreatedAt = s.Now()
		if err := tx.Put("project", p.ID, p); err != nil {
			return nil, err
		}
		if e := tx.Put("authority_policy_migration", "current", struct {
			Version int `json:"version"`
		}{1}); e != nil {
			return nil, e
		}
		return p, s.audit(tx, a, "project.created", p.ID, p.Name)
	})
}
func (s *Service) CreateRequirement(ctx context.Context, a Actor, key string, r domain.Requirement) (json.RawMessage, error) {
	if err := owner(a); err != nil {
		return nil, err
	}
	r.SourceNodeID = ""
	r.Source = nil
	r.OriginalText = ""
	r.AssistantSummary = ""
	if strings.TrimSpace(r.Title) == "" || strings.TrimSpace(r.Body) == "" {
		return nil, domain.Fail("invalid_input", "需求标题和内容不能为空")
	}
	return s.mutate(ctx, a, key, "requirement.create", r, func(tx *store.Tx) (any, error) {
		return s.createRequirement(tx, a, r)
	})
}
func (s *Service) CreateTask(ctx context.Context, a Actor, key string, t domain.Task) (json.RawMessage, error) {
	if err := owner(a); err != nil {
		return nil, err
	}
	if t.Verification != nil {
		return nil, domain.Fail("invalid_input", "use the versioned test-suite operation to create verification tasks")
	}
	if t.BudgetSeconds == 0 {
		t.BudgetSeconds = 14400
	}
	if t.MaxRunSeconds == 0 {
		t.MaxRunSeconds = 3600
	}
	if err := domain.ValidateTask(t); err != nil {
		return nil, err
	}
	if _, err := gitops.NewScope(t.AllowedPaths, nil); err != nil {
		return nil, domain.Fail("invalid_input", err.Error())
	}
	return s.mutateWithCheck(ctx, a, key, "task.create", t, func(tx *store.Tx) error { return baselineGate(tx, nil, false) }, func(tx *store.Tx) (any, error) {
		var p domain.Project
		if e := tx.Get("project", "project", &p); e != nil {
			return nil, e
		}
		for _, id := range t.Dependencies {
			var d domain.Task
			if e := tx.Get("task", id, &d); e != nil {
				return nil, domain.Fail("invalid_input", "依赖任务不存在")
			}
		}
		for _, id := range t.RequirementIDs {
			var r domain.Requirement
			if e := tx.Get("requirement", id, &r); e != nil {
				return nil, domain.Fail("invalid_input", "关联需求不存在")
			}
		}
		if e := validateTestRunLink(tx, t.TestRunID); e != nil {
			return nil, e
		}
		clean := domain.Task{ID: ID("task_"), Version: 1, Title: t.Title, Description: t.Description, Acceptance: t.Acceptance, RequirementIDs: t.RequirementIDs, Dependencies: t.Dependencies, AllowedPaths: t.AllowedPaths, HotResources: t.HotResources, Tool: t.Tool, State: "draft", AuthorizationVersion: 1, BudgetSeconds: t.BudgetSeconds, MaxRunSeconds: t.MaxRunSeconds, MaxRepairs: t.MaxRepairs, MaxRetries: t.MaxRetries, CreatedAt: s.Now(), TestRunID: t.TestRunID}
		if err := tx.Put("task", clean.ID, clean); err != nil {
			return nil, err
		}
		return clean, s.audit(tx, a, "task.created", clean.ID, clean.Title)
	})
}
func (s *Service) TaskAction(ctx context.Context, a Actor, key, id, action string, req domain.VersionRequest) (json.RawMessage, error) {
	if err := owner(a); err != nil {
		return nil, err
	}
	return s.mutateWithCheck(ctx, a, key, "task."+id+"."+action, req, func(tx *store.Tx) error {
		if action == "approve" || action == "resume" {
			return baselineGate(tx, nil, false)
		}
		return nil
	}, func(tx *store.Tx) (any, error) {
		var t domain.Task
		if err := tx.Get("task", id, &t); err != nil {
			return nil, err
		}
		if err := version(t.Version, req.ExpectedVersion); err != nil {
			return nil, err
		}
		switch action {
		case "approve":
			if t.State != "draft" {
				return nil, domain.Fail("conflict", "只能批准草稿任务")
			}
			if e := goalGate(tx, &t, true); e != nil {
				return nil, e
			}
			t.State = "ready"
			t.ApprovedVersion = t.Version + 1
		case "pause":
			if domain.TaskFinished(t) {
				return nil, domain.Fail("conflict", "任务已结束")
			}
			t.HoldReason = "paused"
			if err := requestStop(tx, t.ID); err != nil {
				return nil, err
			}
		case "resume":
			if domain.TaskFinished(t) || t.ApprovedVersion == 0 {
				return nil, domain.Fail("conflict", "任务不可恢复")
			}
			if e := requireQuiescence(tx, t.ID); e != nil {
				return nil, e
			}
			if e := verifyLegacyBudgetResume(tx, &t); e != nil {
				return nil, e
			}
			if t.CurrentCandidateID == "" {
				if e := goalGate(tx, &t, true); e != nil {
					return nil, e
				}
			}
			t.HoldReason = ""
			t.CurrentRunID = ""
			if t.CurrentCandidateID == "" {
				t.State = "ready"
			} else {
				var c domain.Candidate
				if e := tx.Get("candidate", t.CurrentCandidateID, &c); e != nil {
					return nil, e
				}
				// Reconciliation processes an existing publication intent's reality.
				// Other resumes can admit new runs only under the exact current goal.
				if c.State != "reconciling" && c.State != "landing" {
					if e := goalGate(tx, &t, false); e != nil {
						return nil, e
					}
					p, e := getProject(tx)
					if e != nil {
						return nil, e
					}
					if c.TaskID != t.ID || c.AuthorizationVersion != t.AuthorizationVersion || c.PolicyVersion != p.PolicyVersion {
						return nil, domain.Fail("authorization_changed", "candidate authority changed")
					}
				}
				if c.State == "rejected" {
					if t.Repairs >= t.MaxRepairs {
						return nil, domain.Fail("budget", "需要先批准增加修复次数")
					}
					queueRepair(&t, &c, true)
				} else if c.State == "reconciling" {
					if t.Retries >= t.MaxRetries {
						return nil, domain.Fail("budget", "需要先批准增加平台重试次数")
					}
					intents, err := store.List[domain.Intent](tx, "intent")
					if err != nil {
						return nil, err
					}
					for _, intent := range intents {
						if intent.CandidateID == c.ID && intent.State == "needs_attention" {
							intent.State = "unknown"
							if err = tx.Put("intent", intent.ID, intent); err != nil {
								return nil, err
							}
						}
					}
				}
			}
		case "cancel":
			if t.State == "integrated" || t.State == "verified" {
				return nil, domain.Fail("conflict", "已集成任务不能以取消回滚代码")
			}
			t.State = "cancelled"
			t.HoldReason = ""
			if err := requestStop(tx, t.ID); err != nil {
				return nil, err
			}
			if err := releaseResources(tx, t.ID); err != nil {
				return nil, err
			}
		default:
			return nil, domain.Fail("invalid_input", "未知任务操作")
		}
		t.Version++
		if err := tx.Put("task", id, t); err != nil {
			return nil, err
		}
		return t, s.audit(tx, a, "task."+action, id, "")
	})
}
func active(r domain.Run) bool {
	return r.State == "running" || r.State == "starting" || r.State == "cancel_requested"
}
func requestStop(tx *store.Tx, task string) error {
	runs, e := store.List[domain.Run](tx, "run")
	if e != nil {
		return e
	}
	for _, r := range runs {
		if (task == "" || r.TaskID == task) && active(r) {
			r.State = "cancel_requested"
			r.Version++
			if e = tx.Put("run", r.ID, r); e != nil {
				return e
			}
		}
	}
	return nil
}
func releaseResources(tx *store.Tx, task string) error {
	runs, e := store.List[domain.Run](tx, "run")
	if e != nil {
		return e
	}
	for _, run := range runs {
		if run.TaskID == task && !run.Quiescent && (active(run) || run.State == "lost") {
			return nil
		}
	}
	rs, e := store.List[domain.Resource](tx, "resource")
	if e != nil {
		return e
	}
	for _, r := range rs {
		if r.TaskID == task {
			if e = tx.Delete("resource", r.ID); e != nil {
				return e
			}
		}
	}
	return nil
}
func (s *Service) ProjectAction(ctx context.Context, a Actor, key, action string, req domain.VersionRequest) (json.RawMessage, error) {
	if err := owner(a); err != nil {
		return nil, err
	}
	return s.mutate(ctx, a, key, "project."+action, req, func(tx *store.Tx) (any, error) {
		var p domain.Project
		if e := tx.Get("project", "project", &p); e != nil {
			return nil, e
		}
		if e := version(p.Version, req.ExpectedVersion); e != nil {
			return nil, e
		}
		switch action {
		case "pause":
			p.Paused = true
			if e := stopPlanning(tx); e != nil {
				return nil, e
			}
			if e := requestStop(tx, ""); e != nil {
				return nil, e
			}
		case "resume":
			busy, e := initializationBusy(tx)
			if e != nil {
				return nil, e
			}
			if busy {
				return nil, domain.Fail("busy", "初始化进程尚未停止")
			}
			if e := baselineGate(tx, nil, false); e != nil {
				return nil, e
			}
			p.Paused = false
		default:
			return nil, domain.Fail("invalid_input", "未知项目操作")
		}
		p.Version++
		if e := tx.Put("project", p.ID, p); e != nil {
			return nil, e
		}
		return p, s.audit(tx, a, "project."+action, p.ID, "")
	})
}
func (s *Service) Scope(ctx context.Context, a Actor, key, id string, req domain.ScopeRequest) (json.RawMessage, error) {
	if err := owner(a); err != nil {
		return nil, err
	}
	return s.mutateWithCheck(ctx, a, key, "task.scope."+id, req, func(tx *store.Tx) error { return baselineGate(tx, nil, false) }, func(tx *store.Tx) (any, error) {
		var t domain.Task
		if e := tx.Get("task", id, &t); e != nil {
			return nil, e
		}
		if e := version(t.Version, req.ExpectedVersion); e != nil {
			return nil, e
		}
		if t.State == "cancelled" || t.State == "integrated" || t.CurrentRunID != "" {
			return nil, domain.Fail("conflict", "请先停止当前执行再修改授权")
		}
		if e := requireQuiescence(tx, t.ID); e != nil {
			return nil, e
		}
		if t.CurrentCandidateID != "" {
			var c domain.Candidate
			if e := tx.Get("candidate", t.CurrentCandidateID, &c); e != nil {
				return nil, e
			}
			if c.State == "landing" || c.State == "reconciling" {
				return nil, domain.Fail("conflict", "外部更新尚在对账，不能替换授权")
			}
		}
		t.AllowedPaths = req.AllowedPaths
		if e := goalGate(tx, &t, true); e != nil {
			return nil, e
		}
		if _, e := gitops.NewScope(t.AllowedPaths, nil); e != nil {
			return nil, domain.Fail("invalid_input", e.Error())
		}
		if e := domain.ValidateTask(t); e != nil {
			return nil, e
		}
		if req.Reason == "" {
			return nil, domain.Fail("invalid_input", "范围变更需要理由")
		}
		t.AuthorizationVersion++
		t.Version++
		t.CurrentCandidateID = ""
		t.State = "ready"
		if t.ApprovedVersion == 0 {
			t.State = "draft"
		}
		t.HoldReason = ""
		if e := tx.Put("task", id, t); e != nil {
			return nil, e
		}
		return t, s.audit(tx, a, "task.scope", id, req.Reason)
	})
}
func (s *Service) CreatePairing(ctx context.Context, a Actor, key, name string) (json.RawMessage, error) {
	if err := owner(a); err != nil {
		return nil, err
	}
	return s.mutate(ctx, a, key, "pairing.create", name, func(tx *store.Tx) (any, error) {
		code := ID("pair_")
		p := domain.Pairing{ID: Hash(code), CodeHash: Hash(code), Name: name, ExpiresAt: s.Now().Add(10 * time.Minute)}
		if e := tx.Put("pairing", p.ID, p); e != nil {
			return nil, e
		}
		return map[string]any{"code": code, "expires_at": p.ExpiresAt}, s.audit(tx, a, "pairing.created", p.ID, name)
	})
}
func (s *Service) Register(ctx context.Context, req domain.RegisterRequest) (domain.RegisterResponse, error) {
	var out domain.RegisterResponse
	if err := domain.ValidateCapabilities(req.Capabilities); err != nil {
		return out, err
	}
	if req.Name == "" || req.Capacity < 1 || req.Capacity > 2 {
		return out, domain.Fail("invalid_input", "节点名称不能为空，并发容量须为1或2")
	}
	err := s.DB.Update(ctx, func(tx *store.Tx) error {
		var p domain.Pairing
		if e := tx.Get("pairing", Hash(req.Code), &p); e != nil {
			return domain.Fail("unauthorized", "配对码无效")
		}
		if !s.Now().Before(p.ExpiresAt) {
			return domain.Fail("unauthorized", "配对码已过期")
		}
		nodeID := "node_" + Hash(req.Code)[:24]
		token := "node_" + Hash(req.Code+"\x00"+nodeID)
		var n domain.Node
		if p.Used {
			if e := tx.Get("node", nodeID, &n); e != nil {
				return e
			}
			if n.Name != req.Name || n.Platform != req.Platform {
				return domain.Fail("conflict", "配对码已使用")
			}
			out = domain.RegisterResponse{NodeID: nodeID, Token: token}
			return nil
		}
		n = domain.Node{ID: nodeID, Version: 1, Name: req.Name, Platform: req.Platform, Capacity: req.Capacity, Capabilities: req.Capabilities, LastSeen: s.Now()}
		p.Used = true
		if e := tx.Put("node", nodeID, n); e != nil {
			return e
		}
		if e := tx.Put("pairing", p.ID, p); e != nil {
			return e
		}
		if e := tx.Put("credential", Hash(token), domain.Credential{ID: nodeID, Hash: Hash(token), Role: "node", NodeID: nodeID}); e != nil {
			return e
		}
		out = domain.RegisterResponse{NodeID: nodeID, Token: token}
		return s.audit(tx, Actor{ID: nodeID, Role: "node"}, "node.registered", nodeID, req.Platform)
	})
	return out, err
}
func (s *Service) RevokeNode(ctx context.Context, a Actor, key, id string) (json.RawMessage, error) {
	if e := owner(a); e != nil {
		return nil, e
	}
	return s.mutate(ctx, a, key, "node.revoke", id, func(tx *store.Tx) (any, error) {
		var n domain.Node
		if e := tx.Get("node", id, &n); e != nil {
			return nil, e
		}
		n.Revoked = true
		n.Version++
		if e := tx.Put("node", id, n); e != nil {
			return nil, e
		}
		return n, s.audit(tx, a, "node.revoked", id, "")
	})
}
func getProject(tx *store.Tx) (domain.Project, error) {
	var p domain.Project
	e := tx.Get("project", "project", &p)
	return p, e
}
func (s *Service) Project(ctx context.Context) (domain.Project, error) {
	var p domain.Project
	e := s.DB.View(ctx, func(tx *store.Tx) error { var e error; p, e = getProject(tx); return e })
	return p, e
}
func limit(values ...int64) int64 {
	v := values[0]
	for _, n := range values[1:] {
		if n < v {
			v = n
		}
	}
	return v
}
func errDetail(err error) string {
	if err == nil {
		return ""
	}
	v := err.Error()
	if len(v) > 4096 {
		return v[:4096]
	}
	return v
}
