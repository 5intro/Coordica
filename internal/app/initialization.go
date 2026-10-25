package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/jsoninput"
	"coordica/internal/store"
)

// AnalysisSource is owned by the initialization consumer. Implementations know
// Git objects, not permissions, transactions or project lifecycle.
type AnalysisSource interface {
	AnalysisRevision(context.Context, string, string) (string, error)
	ReadAnalysis(context.Context, string, string) (gitops.AnalysisSnapshot, error)
}

func (s *Service) analysisSource() AnalysisSource {
	if s.Source != nil {
		if g, ok := s.Source.(*gitops.Client); ok {
			return g
		}
		return s.Source
	}
	return gitops.New("git").WithAnalysisPrivatePaths([]string{s.Dir})
}
func initialization(tx *store.Tx) (domain.Initialization, error) {
	var v domain.Initialization
	err := tx.Get("initialization", "current", &v)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Initialization{Status: "draft", AvailablePaths: []string{}, Feedback: []domain.InitializationFeedback{}, CoveredPaths: []string{}, Omissions: []string{}}, nil
	}
	if err == nil && v.OperationID != "" {
		var op domain.InitializationOperation
		if e := tx.Get("initialization_operation", v.OperationID, &op); e != nil {
			return v, e
		}
		v.OperationGeneration = op.Generation
		v.OperationVersion = op.Version
		v.OperationState = op.State
	}
	return v, err
}
func initializationPolicy(p domain.Project) string {
	b, _ := json.Marshal(struct {
		Repository, Source, Branch string
		Policy                     int64
		Protected                  []string
		Checks                     []domain.Check
	}{p.RepositoryURL, p.SourceRepository, p.TargetBranch, p.PolicyVersion, p.ProtectedPaths, p.Checks})
	return Hash(string(b))
}
func initializationDigest(v domain.Initialization) string {
	b, _ := json.Marshal(struct {
		Version                       int64
		Kind, SHA, Policy             string
		Available, Covered, Omissions []string
		Result                        *domain.InitializationResult
		Feedback                      []domain.InitializationFeedback
		Leader                        *domain.LeaderSelection `json:"leader,omitempty"`
	}{v.Version, v.Kind, v.SourceSHA, v.PolicyDigest, v.AvailablePaths, v.CoveredPaths, v.Omissions, v.Result, v.Feedback, v.Leader})
	return Hash(string(b))
}
func baselineGate(tx *store.Tx, t *domain.Task, bind bool) error {
	v, e := initialization(tx)
	if e != nil {
		return e
	}
	p, e := getProject(tx)
	if e != nil {
		return e
	}
	if v.Status != "initialized" || v.Version < 1 || v.ApprovedVersion != v.Version || v.Digest == "" || v.ApprovedDigest != v.Digest || v.Digest != initializationDigest(v) || v.ApprovedBaseline == nil || len(p.Checks) == 0 || v.PolicyDigest != initializationPolicy(p) {
		return domain.Fail("initialization_required", "项目需要 Leader 初始化与 Owner 确认精确基线版本")
	}
	if v.Result == nil || !v.Result.Ready {
		return domain.Fail("initialization_required", "approved structured baseline missing")
	}
	normative, _ := json.Marshal(v.ApprovedBaseline)
	proposed, _ := json.Marshal(v.Result.Baseline)
	if string(normative) != string(proposed) {
		return domain.Fail("baseline_changed", "approved normative context differs from confirmed digest")
	}
	if t != nil {
		if t.Tool != "codex" && !(t.Tool == "program" && t.Verification != nil) {
			return domain.Fail("invalid_input", "development requires native codex; other registered tools are test/review-only")
		}
		rootAll := false
		for _, root := range v.ApprovedBaseline.AllowedRoots {
			if root == "." {
				rootAll = true
			}
		}
		if !rootAll {
			if e = literalScope(t.AllowedPaths, v.ApprovedBaseline.AllowedRoots); e != nil {
				return e
			}
		}
		if bind {
			t.BaselineVersion, t.BaselineDigest = v.Version, v.Digest
		} else if t.BaselineVersion != v.Version || t.BaselineDigest != v.Digest {
			return domain.Fail("baseline_changed", "任务所属共享基线已变化，需要重新授权")
		}
	}
	return nil
}
func baselineRun(tx *store.Tx, r domain.Run) error {
	var t domain.Task
	if e := tx.Get("task", r.TaskID, &t); e != nil {
		return e
	}
	if e := baselineGate(tx, &t, false); e != nil {
		return e
	}
	if r.AuthorizationVersion != t.AuthorizationVersion || r.Generation != t.Generation || r.GoalVersion != t.GoalVersion || r.GoalAuthorizationVersion != t.GoalAuthorizationVersion || r.BaselineVersion != t.BaselineVersion || r.BaselineDigest != t.BaselineDigest {
		return domain.Fail("baseline_changed", "Run 共享基线不匹配")
	}
	return nil
}
func initializationQuiet(tx *store.Tx) error {
	p, e := getProject(tx)
	if e != nil {
		return e
	}
	if !p.Paused {
		return domain.Fail("pause_required", "请先暂停项目并等待所有执行停止")
	}
	if pending, e := publicationRevocationsPending(tx); e != nil {
		return e
	} else if pending {
		return domain.Fail("busy", "GitHub 旧评审状态尚未撤销，等待发布对账")
	}
	runs, e := store.List[domain.Run](tx, "run")
	if e != nil {
		return e
	}
	for _, r := range runs {
		if active(r) || !r.Quiescent && r.State == "lost" {
			return domain.Fail("busy", "存在运行或未知存活执行，不能重新初始化")
		}
	}
	planning, e := store.List[domain.PlanningRun](tx, "planning_run")
	if e != nil {
		return e
	}
	for _, r := range planning {
		if r.State == "unknown_live" && !r.TreeStopped || !r.Settled && (r.State == "queued" || r.State == "running" || r.State == "unknown_live" || r.State == "pending_result" || r.State == "cancel_requested") {
			return domain.Fail("busy", "Leader 请求尚未停止")
		}
	}
	candidates, e := store.List[domain.Candidate](tx, "candidate")
	if e != nil {
		return e
	}
	for _, c := range candidates {
		if c.State != "landed" && c.State != "rejected" && c.State != "invalidated" {
			return domain.Fail("busy", "待处理候选必须先安全结束")
		}
	}
	intents, e := store.List[domain.Intent](tx, "intent")
	if e != nil {
		return e
	}
	for _, in := range intents {
		if in.State == "prepared" || in.State == "unknown" || in.State == "dispatching" || in.State == "needs_attention" {
			return domain.Fail("busy", "Git 发布意图需要先恢复")
		}
	}
	return nil
}
func (s *Service) Initialization(ctx context.Context, a Actor) (domain.Initialization, error) {
	var v domain.Initialization
	e := s.DB.View(ctx, func(tx *store.Tx) error {
		if e := conversationCheck(a)(tx); e != nil {
			return e
		}
		var e error
		v, e = initialization(tx)
		return e
	})
	if e == nil && a.Role == "chat" {
		v = safeInitialization(v)
	}
	return v, e
}
func (s *Service) RequestInitialization(ctx context.Context, a Actor, key string, req domain.InitializationRequest) (json.RawMessage, error) {
	if req.Kind != "existing" && req.Kind != "new" {
		return nil, domain.Fail("invalid_input", "kind must be existing or new")
	}
	if (req.Leader == nil) != (req.LeaderVersion == nil) {
		return nil, domain.Fail("invalid_input", "Leader selection requires its exact configuration version")
	}
	if req.Leader != nil {
		if err := owner(a); err != nil {
			return nil, err
		}
	}
	if raw, found, err := replayMutation(ctx, s, a, key, "initialization.request", req, conversationCheck(a)); found || err != nil {
		return initializationResponse(a, raw, err)
	}
	if req.Leader != nil {
		if err := (LeaderConfigurations{s}).validate(ctx, *req.Leader); err != nil {
			return nil, err
		}
	}
	var p domain.Project
	e := s.DB.View(ctx, func(tx *store.Tx) error {
		if e := conversationCheck(a)(tx); e != nil {
			return e
		}
		var e error
		p, e = getProject(tx)
		return e
	})
	if e != nil {
		return nil, e
	}
	inputCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	sha, e := s.analysisSource().AnalysisRevision(inputCtx, analysisRepository(p), p.TargetBranch)
	if req.Kind == "new" {
		if !errors.Is(e, gitops.ErrUnborn) {
			if e == nil {
				return nil, domain.Fail("analysis_input", "registered repository already has a commit; existing-source analysis is mandatory")
			}
			return nil, domain.Fail("analysis_input", e.Error())
		}
		sha = ""
	} else if e != nil {
		return nil, domain.Fail("analysis_input", e.Error())
	}

	raw, err := s.mutateWithCheck(ctx, a, key, "initialization.request", req, conversationCheck(a), func(tx *store.Tx) (any, error) {
		v, e := initialization(tx)
		if e != nil {
			return nil, e
		}
		if e = version(v.Version, req.ExpectedVersion); e != nil {
			return nil, e
		}
		current, e := getProject(tx)
		if e != nil {
			return nil, e
		}
		if initializationPolicy(current) != initializationPolicy(p) {
			return nil, domain.Fail("version_conflict", "仓库输入或策略已变化")
		}
		if e = initializationQuiet(tx); e != nil {
			return nil, e
		}
		if v.OperationID != "" {
			var op domain.InitializationOperation
			if e = tx.Get("initialization_operation", v.OperationID, &op); e != nil {
				return nil, e
			}
			if !op.Settled {
				return nil, domain.Fail("busy", "初始化请求尚未停止")
			}
		}
		if req.Leader != nil {
			if _, err := (LeaderConfigurations{s}).apply(tx, a, domain.LeaderConfigureRequest{
				ExpectedVersion: *req.LeaderVersion, Selection: *req.Leader,
			}); err != nil {
				return nil, err
			}
		}
		v.Version++
		v.AvailablePaths = []string{}
		v.Kind = req.Kind
		v.SourceSHA = sha
		v.PolicyDigest = initializationPolicy(p)
		v.Status = "analyzing"
		v.Result = nil
		v.Digest = ""
		v.Detail = ""
		v.CoveredPaths = []string{}
		v.Omissions = []string{}
		v.ApprovedVersion = 0
		v.ApprovedDigest = ""
		v.ApprovedBaseline = nil
		if e = s.invalidateBaselineTasks(tx); e != nil {
			return nil, e
		}
		return s.queueInitialization(tx, a, v)
	})
	return initializationResponse(a, raw, err)
}
func (s *Service) invalidateBaselineTasks(tx *store.Tx) error {
	tasks, e := store.List[domain.Task](tx, "task")
	if e != nil {
		return e
	}
	for _, t := range tasks {
		if domain.TaskFinished(t) {
			continue
		}
		t.ApprovedVersion = 0
		t.AuthorizationVersion++
		t.Generation++
		t.Version++
		t.State = "draft"
		t.HoldReason = "baseline_changed"
		if e = tx.Put("task", t.ID, t); e != nil {
			return e
		}
		if e = releaseResources(tx, t.ID); e != nil {
			return e
		}
	}
	return nil
}
func (s *Service) queueInitialization(tx *store.Tx, a Actor, v domain.Initialization) (any, error) {
	settings, err := automation(tx)
	if err != nil {
		return nil, err
	}
	op := domain.InitializationOperation{
		ID: ID("init_"), ActorID: a.ID, ActorRole: a.Role, Version: 1, InitializationVersion: v.Version, Generation: 1,
		State: "queued", GrantSeconds: settings.PlanningTimeoutSeconds, MaxRetries: settings.PlanningMaxRetries,
	}
	op.Leader = leaderChoice(settings)
	v.Leader = leaderChoice(settings)
	v.OperationID = op.ID
	v.OperationGeneration = op.Generation
	v.OperationVersion = op.Version
	v.OperationState = op.State
	v.Status = "analyzing"
	if e := tx.Put("initialization_operation", op.ID, op); e != nil {
		return nil, e
	}
	if e := tx.Put("initialization", "current", v); e != nil {
		return nil, e
	}
	return v, s.audit(tx, a, "initialization.requested", op.ID, fmt.Sprint(v.Version))
}
func (s *Service) AnswerInitialization(ctx context.Context, a Actor, key string, req domain.InitializationAnswer) (json.RawMessage, error) {
	source, sourceErr := sourceInput(a, req.Source)
	if sourceErr != nil {
		return nil, sourceErr
	}
	req.Source = source
	if !boundedText(req.OriginalText, 16384, true) || !boundedText(req.QuestionID, 128, false) || domain.ValidateChatSource(req.Source) != nil {
		return nil, domain.Fail("invalid_input", "需要原始反馈与来源标识")
	}
	raw, err := s.mutateWithCheck(ctx, a, key, "initialization.answer", req, conversationCheck(a), func(tx *store.Tx) (any, error) {
		if replay, ok, e := chatSourceReplay(tx, "initialization.answer", req.Source, req); e != nil || ok {
			return replay, e
		}
		v, e := initialization(tx)
		if e != nil {
			return nil, e
		}
		if e = version(v.Version, req.ExpectedVersion); e != nil {
			return nil, e
		}
		if v.Status != "interview" && v.Status != "awaiting_owner" {
			return nil, domain.Fail("conflict", "只有待讨论草案可提交反馈；批准基线需要显式重新初始化")
		}
		if e = initializationQuiet(tx); e != nil {
			return nil, e
		}
		if req.QuestionID != "" {
			found := false
			for _, q := range v.Result.Questions {
				if q.ID == req.QuestionID {
					found = true
				}
			}
			if !found {
				return nil, domain.Fail("invalid_input", "反馈问题不属于当前 frontier")
			}
		}
		total := len(req.OriginalText)
		for _, f := range v.Feedback {
			total += len(f.OriginalText)
		}
		if total > 256<<10 || len(v.Feedback) >= 64 {
			return nil, domain.Fail("invalid_input", "interview context limit reached; Owner must review and restart with settled materials")
		}
		if e = tx.Put("initialization_analysis", fmt.Sprint(v.Version), v); e != nil {
			return nil, e
		}
		v.Feedback = append(v.Feedback, domain.InitializationFeedback{ID: ID("feedback_"), Version: v.Version, ActorID: a.ID, QuestionID: req.QuestionID, OriginalText: req.OriginalText, Source: req.Source, CreatedAt: s.Now()})
		v.Version++
		v.Digest = ""
		v.Detail = ""
		response, e := s.queueInitialization(tx, a, v)
		if e != nil {
			return nil, e
		}
		if e = rememberChatSource(tx, "initialization.answer", req.Source, req, response); e != nil {
			return nil, e
		}
		return response, nil
	})
	return initializationResponse(a, raw, err)
}
func (s *Service) ConfirmInitialization(ctx context.Context, a Actor, key string, req domain.InitializationConfirm) (json.RawMessage, error) {
	if e := owner(a); e != nil {
		return nil, e
	}
	v, e := s.Initialization(ctx, a)
	if e != nil {
		return nil, e
	}
	p, e := s.Project(ctx)
	if e != nil {
		return nil, e
	}
	inputCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	sha, inputErr := s.analysisSource().AnalysisRevision(inputCtx, analysisRepository(p), p.TargetBranch)
	if v.Kind == "existing" && (inputErr != nil || sha != v.SourceSHA) || v.Kind == "new" && !errors.Is(inputErr, gitops.ErrUnborn) {
		return nil, domain.Fail("version_conflict", "初始化输入已变化；请重新分析")
	}
	return s.mutateWithCheck(ctx, a, key, "initialization.confirm", req, func(tx *store.Tx) error {
		current, e := initialization(tx)
		if e != nil {
			return e
		}
		p, e := getProject(tx)
		if e != nil {
			return e
		}
		if current.Version != req.ExpectedVersion || current.Digest != req.Digest || initializationPolicy(p) != current.PolicyDigest || current.SourceSHA != v.SourceSHA {
			return domain.Fail("version_conflict", "confirmation no longer matches reviewed input")
		}
		return nil
	}, func(tx *store.Tx) (any, error) {
		current, e := initialization(tx)
		if e != nil {
			return nil, e
		}
		if e = version(current.Version, req.ExpectedVersion); e != nil {
			return nil, e
		}
		p, e := getProject(tx)
		if e != nil {
			return nil, e
		}
		if current.Status != "awaiting_owner" || current.Result == nil || !current.Result.Ready || req.Digest == "" || current.Digest != req.Digest || current.Digest != initializationDigest(current) || initializationPolicy(p) != current.PolicyDigest || v.SourceSHA != current.SourceSHA {
			return nil, domain.Fail("version_conflict", "请确认当前已审阅精确草案版本与摘要")
		}
		if e = initializationQuiet(tx); e != nil {
			return nil, e
		}
		if e = baselinePolicyAgreement(current.Result.Baseline, p); e != nil {
			return nil, e
		}
		current.Status = "initialized"
		current.ApprovedVersion = current.Version
		current.ApprovedDigest = current.Digest
		b := current.Result.Baseline
		current.ApprovedBaseline = &b
		if e = tx.Put("initialization", "current", current); e != nil {
			return nil, e
		}
		if e = tx.Put("initialization_baseline", fmt.Sprint(current.Version), current); e != nil {
			return nil, e
		}
		return current, s.audit(tx, a, "initialization.confirmed", "project", current.Digest)
	})
}
func decodeInitialization(text string, v domain.Initialization) (domain.InitializationResult, error) {
	var r domain.InitializationResult
	if e := jsoninput.Decode([]byte(text), &r, 128<<10); e != nil {
		return r, e
	}
	if e := jsoninput.Required([]byte(text), "generation", "initialization_version", "operation_id", "source_sha", "ready", "inspected_paths", "facts", "suggestions", "questions", "baseline"); e != nil {
		return r, e
	}
	if r.Generation != v.OperationGeneration || r.InitializationVersion != v.Version || r.OperationID != v.OperationID || r.SourceSHA != v.SourceSHA {
		return r, errors.New("model result belongs to another initialization operation/version/source")
	}
	b := r.Baseline
	for _, field := range []string{b.Requirements, b.Acceptance, b.Architecture, b.Modules, b.Conventions, b.Testing, b.Functionality, b.Risks} {
		if !boundedText(field, 16384, r.Ready) {
			return r, errors.New("invalid or incomplete baseline")
		}
	}
	if r.InspectedPaths == nil || r.Facts == nil || r.Questions == nil || r.Suggestions == nil || len(r.Questions) > 20 || len(r.Facts) > 200 || len(r.Suggestions) > 50 {
		return r, errors.New("missing or oversize structured analysis")
	}
	if r.Ready && (len(r.Questions) != 0 || len(b.AllowedRoots) == 0 || len(b.Checks) == 0 || b.ProtectedPaths == nil) {
		return r, errors.New("ready result has unresolved decisions or missing policy")
	}
	if len(b.AllowedRoots) > 0 {
		if e := validateRoots(b.AllowedRoots); e != nil {
			return r, e
		}
	}
	if _, e := gitops.NewScope([]string{"**"}, b.ProtectedPaths); e != nil {
		return r, e
	}
	for _, check := range b.Checks {
		if strings.TrimSpace(check.Name) == "" || strings.TrimSpace(check.Command) == "" || check.TimeoutSeconds < 1 || check.TimeoutSeconds > 3600 {
			return r, errors.New("invalid proposed check")
		}
	}
	covered := map[string]bool{}
	for _, path := range v.AvailablePaths {
		covered[path] = true
	}
	inspected := map[string]bool{}
	for _, path := range r.InspectedPaths {
		if inspected[path] {
			return r, errors.New("duplicate inspected path")
		}
		inspected[path] = true
	}
	for _, path := range r.InspectedPaths {
		if !covered[path] {
			return r, errors.New("inspection outside supplied source")
		}
	}
	if r.Ready && v.Kind == "existing" && len(v.AvailablePaths) == 0 {
		return r, errors.New("missing analysis source manifest")
	}
	if r.Ready && v.Kind == "existing" {
		modules := map[string]bool{}
		for _, path := range v.AvailablePaths {
			dir := filepath.ToSlash(filepath.Dir(path))
			modules[dir] = false
		}
		for _, path := range r.InspectedPaths {
			modules[filepath.ToSlash(filepath.Dir(path))] = true
		}
		for dir, read := range modules {
			if !read {
				return r, fmt.Errorf("unresolved source coverage for %s", dir)
			}
		}
	}
	if v.Kind == "existing" && (len(r.Facts) == 0 || len(r.InspectedPaths) == 0) {
		return r, errors.New("existing repository analysis needs tracked evidence")
	}
	for _, fact := range r.Facts {
		if !boundedText(fact.Fact, 4096, true) || v.Kind == "existing" && !inspected[fact.Path] {
			return r, errors.New("analysis evidence not in supplied snapshot")
		}
	}
	settled := map[string]bool{}
	for _, f := range v.Feedback {
		if f.QuestionID != "" {
			settled[f.QuestionID] = true
		}
	}
	seen := map[string]bool{}
	for _, q := range r.Questions {
		if !domain.ChatIdentifier.MatchString(q.ID) || seen[q.ID] || settled[q.ID] || !boundedText(q.Text, 4096, true) {
			return r, errors.New("invalid or repeated interview frontier")
		}
		seen[q.ID] = true
		for _, dependency := range q.Prerequisites {
			if !settled[dependency] {
				return r, errors.New("frontier depends on an unsettled decision")
			}
		}
	}
	return r, nil
}

// Baseline records the enforced policy, not an implicit model policy change.
func baselinePolicyAgreement(b domain.Baseline, p domain.Project) error {
	if len(p.Checks) == 0 || len(p.Checks) != len(b.Checks) {
		return domain.Fail("policy_conflict", "baseline requires explicit active checks")
	}
	same := true
	for i, c := range p.Checks {
		q := b.Checks[i]
		if c.Name != q.Name || c.Command != q.Command || c.TimeoutSeconds != q.TimeoutSeconds || len(c.Args) != len(q.Args) {
			same = false
			break
		}
		for j, arg := range c.Args {
			if arg != q.Args[j] {
				same = false
			}
		}
	}
	if !same {
		return domain.Fail("policy_conflict", "proposed checks differ from active policy; Owner must update policy explicitly and reanalyze")
	}
	actual := map[string]bool{}
	for _, path := range p.ProtectedPaths {
		actual[path] = true
	}
	if len(actual) != len(b.ProtectedPaths) {
		return domain.Fail("policy_conflict", "baseline protected paths must match active policy")
	}
	seen := map[string]bool{}
	for _, path := range b.ProtectedPaths {
		if seen[path] || !actual[path] {
			return domain.Fail("policy_conflict", "baseline protected paths differ from active policy")
		}
		seen[path] = true
	}
	return nil
}
func initializationIssuer(tx *store.Tx, op domain.InitializationOperation) error {
	if op.ActorRole == "owner" {
		return nil
	}
	return conversationCheck(Actor{ID: op.ActorID, Role: op.ActorRole})(tx)
}

func analysisRepository(p domain.Project) string {
	if p.SourceRepository != "" {
		return p.SourceRepository
	}
	return p.RepositoryURL
}
