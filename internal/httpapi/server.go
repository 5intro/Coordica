// Package httpapi decodes the versioned protocol and delegates business rules.
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"coordica/internal/app"
	"coordica/internal/domain"
	"coordica/internal/jsoninput"
	"coordica/internal/localhttp"
)

type HostLifecycle interface {
	Initialize(context.Context) (domain.HostLifecycleStatus, error)
	Status(context.Context) (domain.HostLifecycleStatus, error)
}
type Server struct {
	Host           HostLifecycle
	Service        *app.Service
	Integration    *app.Integrator
	UI             fs.FS
	ConnectionInfo domain.ConnectionInfo
}

// Handler combines console and runner operations for a loopback listener.
// Separate listeners must use AdminHandler and RunnerHandler instead.
func (s *Server) Handler() http.Handler { return localhttp.Guard(s.handler(true, true)) }

// AdminHandler serves the Web console and owner API. Its listener must be bound
// to loopback; node credentials are never accepted on this surface.
func (s *Server) AdminHandler() http.Handler { return localhttp.Guard(s.handler(false, true)) }

// RunnerHandler exposes only pairing and node execution operations. It may be
// mounted on the center's authenticated HTTPS listener for remote runners.
func (s *Server) RunnerHandler() http.Handler { return s.handler(true, false) }

func (s *Server) handler(runners, admin bool) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		value, e := s.Service.PublicHealth(r.Context())
		respond(w, value, e)
	})
	if admin {
		mux.HandleFunc("GET /api/v1/local/info", func(w http.ResponseWriter, r *http.Request) {
			respond(w, domain.LocalInfo{Mode: "center", CenterConsoleURL: s.ConnectionInfo.ConsoleURL}, nil)
		})
	}
	if runners {
		mux.HandleFunc("POST /api/v1/runner/register", s.register)
	}
	if admin {
		registerAdminWorkspaces(mux, s)
	}
	chatAPI := s.auth("chat", func(w http.ResponseWriter, r *http.Request) {
		a := r.Context().Value(actorKey{}).(app.Actor)
		if !admin && a.ChatScope != domain.ChatScopeNode || !runners && a.ChatScope != domain.ChatScopeCenter {
			respond(w, nil, domain.Fail("forbidden", "chat credential does not match listener audience"))
			return
		}
		s.chatAPI(w, r)
	})
	ownerAPI, nodeAPI := s.auth("owner", s.api), s.auth("node", s.api)
	mux.HandleFunc("/api/v1/", func(w http.ResponseWriter, r *http.Request) {
		// Check the transport audience before authentication: the network
		// listener has no owner API, even when a valid owner token is supplied.
		if chatRoute(r) {
			chatAPI(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/chat/") {
			http.NotFound(w, r)
			return
		}
		if runnerRoute(r) {
			if runners {
				nodeAPI(w, r)
				return
			}
		} else if admin {
			ownerAPI(w, r)
			return
		}
		http.NotFound(w, r)
	})
	if !admin {
		mux.HandleFunc("/", http.NotFound)
	} else if s.UI != nil {
		files := http.FileServer(http.FS(s.UI))
		mux.Handle("/", files)
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Web assets unavailable; build web first", http.StatusServiceUnavailable)
		})
	}
	return securedConsole(mux)
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var in domain.RegisterRequest
	if e := decode(r, &in); e != nil {
		respond(w, nil, e)
		return
	}
	// The one-time pairing code is authenticated by the shared use case.
	v, e := s.Service.Register(r.Context(), in)
	respond(w, v, e)
}

// runnerRoute is the complete network execution surface. Unknown methods and
// paths never fall through to owner dispatch when new admin operations appear.
func runnerRoute(r *http.Request) bool {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	if r.Method == http.MethodGet && (path == "runner/state" || path == "runner/local-entry") {
		return true
	}
	if r.Method == http.MethodPost && (path == "runner/register" || path == "runner/claim" || path == "runner/presence" || path == "runner/requirements" || path == "runner/chat-credentials") {
		return true
	}
	parts := strings.Split(path, "/")
	if r.Method == http.MethodPost && len(parts) == 4 && parts[0] == "runner" && parts[1] == "chat-credentials" && parts[2] != "" && parts[3] == "revoke" {
		return true
	}
	if len(parts) < 2 || parts[0] != "runs" || parts[1] == "" {
		return false
	}
	if r.Method == http.MethodGet {
		return len(parts) == 2 || len(parts) == 3 && (parts[2] == "context" || parts[2] == "input.bundle")
	}
	if r.Method == http.MethodPost && len(parts) == 3 {
		switch parts[2] {
		case "heartbeat", "complete", "artifacts", "block", "request_scope":
			return true
		}
	}
	return false
}

type actorKey struct{}

func (s *Server) auth(role string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw := r.Header.Get("Authorization")
		if !strings.HasPrefix(raw, "Bearer ") {
			respond(w, nil, domain.Fail("unauthorized", "请输入访问令牌"))
			return
		}
		a, e := s.Service.Authenticate(r.Context(), strings.TrimPrefix(raw, "Bearer "))
		if e != nil {
			respond(w, nil, e)
			return
		}
		if a.Role != role {
			respond(w, nil, domain.Fail("forbidden", "访问凭据不适用于此接口"))
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), actorKey{}, a)))
	}
}
func (s *Server) api(w http.ResponseWriter, r *http.Request) {
	a := r.Context().Value(actorKey{}).(app.Actor)
	ctx := r.Context()
	key := r.Header.Get("Idempotency-Key")
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	parts := strings.Split(path, "/")
	var result any
	var err error
	if r.Method == http.MethodGet {
		switch {
		case path == "leader/configuration":
			result, err = (app.LeaderConfigurations{Service: s.Service}).Read(ctx, a)
		case path == "tests/readiness":
			result, err = s.Service.TestReadiness(ctx, a, r.URL.Query().Get("commit"))
		case path == "system/host":
			if s.Host == nil {
				err = domain.Fail("host_setup_required", "trusted center host configuration required")
			} else {
				result, err = s.Host.Status(ctx)
			}
		case path == "system/identity":
			var status domain.CenterRuntimeStatus
			status, err = s.Service.CenterStatus(ctx, a)
			result = status.Identity
		case path == "system/host-status":
			result, err = s.Service.CenterStatus(ctx, a)
		case path == "initialization/operation":
			result, err = s.Service.InitializationOperation(ctx, a)
		case path == "initialization":
			result, err = s.Service.Initialization(ctx, a)
		case path == "state":
			result, err = s.Service.Snapshot(ctx, a)
		case len(parts) == 2 && parts[0] == "requirements":
			var before int64
			if text := r.URL.Query().Get("before"); text != "" {
				before, err = strconv.ParseInt(text, 10, 64)
				if err != nil {
					err = domain.Fail("invalid_input", "历史游标无效")
				}
			}
			if err == nil {
				result, err = s.Service.RequirementDetail(ctx, a, parts[1], before)
			}
		case path == "export":
			result, err = s.Service.Export(ctx, a)
		case path == "system/connection":
			result = s.ConnectionInfo
		case path == "runner/state":
			result, err = s.Service.NodeSnapshot(ctx, a)
		case path == "runner/local-entry":
			result = domain.CenterEntry{ConsoleURL: s.ConnectionInfo.ConsoleURL, RunnerURL: s.ConnectionInfo.RunnerURL}
		case len(parts) == 3 && parts[0] == "runs" && parts[2] == "context":
			result, err = s.Service.RunContext(ctx, a, parts[1])
		case len(parts) == 2 && parts[0] == "runs":
			result, err = s.Service.AssignmentRun(ctx, a, parts[1])
		case len(parts) == 3 && parts[0] == "runs" && parts[2] == "input.bundle":
			var run domain.Run
			run, err = s.Service.InputRun(ctx, a, parts[1])
			if err == nil {
				var file string
				file, err = s.Integration.InputBundle(ctx, run)
				if err == nil {
					w.Header().Set("Content-Type", "application/octet-stream")
					http.ServeFile(w, r, file)
					return
				}
			}
		default:
			err = domain.ErrNotFound
		}
		respond(w, result, err)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", 405)
		return
	}
	switch {
	case len(parts) == 3 && parts[0] == "requirements" && parts[2] == "reassess":
		var in domain.VersionRequest
		if err = decode(r, &in); err == nil {
			result, err = s.Service.ReassessRequirement(ctx, a, key, parts[1], in)
		}
	case len(parts) == 3 && parts[0] == "requirements" && (parts[2] == "revise" || parts[2] == "withdraw"):
		var in domain.RequirementEdit
		if err = decode(r, &in); err == nil {
			result, err = s.Service.EditRequirement(ctx, a, key, parts[1], parts[2], in)
		}
	case path == "tests/suites":
		var in domain.TestSuiteSave
		if err = decode(r, &in); err == nil {
			result, err = s.Service.SaveTestSuite(ctx, a, key, in)
		}
	case path == "tests/run":
		var in domain.VerificationRequest
		if err = decode(r, &in); err == nil {
			result, err = s.Service.StartVerification(ctx, a, key, in)
		}
	case len(parts) == 3 && parts[0] == "requirements" && parts[2] == "link":
		var in domain.RequirementLinkRequest
		if err = decode(r, &in); err == nil {
			result, err = s.Service.LinkRequirements(ctx, a, key, parts[1], in)
		}
	case path == "system/host/initialize" || path == "system/host/start":
		var in struct{}
		if err = decodeChat(r, &in); err == nil {
			if s.Host == nil {
				err = domain.Fail("host_setup_required", "trusted center host configuration required")
			} else {
				result, err = s.Host.Initialize(ctx)
			}
		}
	case path == "initialization/cancel":
		var in domain.VersionRequest
		if err = decode(r, &in); err == nil {
			result, err = s.Service.CancelInitialization(ctx, a, key, in)
		}
	case path == "initialization/confirm-stopped":
		var in domain.StopConfirmation
		if err = decode(r, &in); err == nil {
			result, err = s.Service.ConfirmInitializationStopped(ctx, a, key, in)
		}
	case path == "initialization/analyze":
		var in domain.InitializationRequest
		if err = decode(r, &in); err == nil {
			result, err = s.Service.RequestInitialization(ctx, a, key, in)
		}
	case path == "initialization/answer":
		var in domain.InitializationAnswer
		if err = decode(r, &in); err == nil {
			result, err = s.Service.AnswerInitialization(ctx, a, key, in)
		}
	case path == "initialization/confirm":
		var in domain.InitializationConfirm
		if err = decode(r, &in); err == nil {
			result, err = s.Service.ConfirmInitialization(ctx, a, key, in)
		}
	case path == "runner/chat-credentials":
		var in domain.ChatCredentialRequest
		if err = decodeChat(r, &in); err == nil {
			result, err = s.Service.CreateNodeChatCredential(ctx, a, key, in)
		}
	case len(parts) == 4 && parts[0] == "runner" && parts[1] == "chat-credentials" && parts[3] == "revoke":
		result, err = s.Service.RevokeChatCredential(ctx, a, key, parts[2])
	case path == "chat-credentials":
		var in domain.ChatCredentialRequest
		if err = decodeChat(r, &in); err == nil {
			result, err = s.Service.CreateChatCredential(ctx, a, key, in)
		}
	case len(parts) == 3 && parts[0] == "chat-credentials" && parts[2] == "revoke":
		result, err = s.Service.RevokeChatCredential(ctx, a, key, parts[1])
	case path == "planning/consult" || path == "planning/propose":
		var in domain.ConversationInput
		if err = decodeChat(r, &in); err == nil {
			kind := "consultation"
			if path == "planning/propose" {
				kind = "proposal"
			}
			result, err = s.Service.ConsultLeader(ctx, a, key, kind, in)
		}
	case path == "planning/consult-cancel":
		var in domain.ChatCancelRequest
		if err = decodeChat(r, &in); err == nil {
			result, err = s.Service.CancelConversation(ctx, a, key, in)
		}
	case len(parts) == 3 && parts[0] == "consultation-runs" && parts[2] == "confirm-stopped":
		var in domain.StopConfirmation
		if err = decodeChat(r, &in); err == nil {
			result, err = s.Service.ConfirmConsultationStopped(ctx, a, key, parts[1], in)
		}
	case path == "system/automation":
		var in domain.AutomationRequest
		if err = decode(r, &in); err == nil {
			result, err = s.Service.ConfigureAutomation(ctx, a, key, in)
		}
	case path == "leader/configuration":
		var in domain.LeaderConfigureRequest
		if err = decode(r, &in); err == nil {
			result, err = (app.LeaderConfigurations{Service: s.Service}).Configure(ctx, a, key, in)
		}
	case path == "planning/save":
		var in domain.PlanningSaveRequest
		if err = decode(r, &in); err == nil {
			result, err = s.Service.SavePlanning(ctx, a, key, in)
		}
	case path == "planning/authorize":
		var in domain.VersionRequest
		if err = decode(r, &in); err == nil {
			result, err = s.Service.AuthorizePlanning(ctx, a, key, in)
		}
	case path == "planning/messages" || path == "planning/evaluate":
		var in domain.PlanningMessageRequest
		if err = decode(r, &in); err == nil {
			result, err = s.Service.SchedulePlanning(ctx, a, key, in)
		}
	case path == "planning/cancel":
		var in domain.VersionRequest
		if err = decode(r, &in); err == nil {
			result, err = s.Service.CancelPlanning(ctx, a, key, in)
		}
	case len(parts) == 3 && parts[0] == "nodes" && parts[2] == "configure":
		var in domain.NodeConfigureRequest
		if err = decode(r, &in); err == nil {
			result, err = s.Service.ConfigureNode(ctx, a, key, parts[1], in)
		}
	case len(parts) == 3 && parts[0] == "decisions" && parts[2] == "resolve":
		var in domain.DecisionResolveRequest
		if err = decode(r, &in); err == nil {
			result, err = s.Service.ResolveDecision(ctx, a, key, parts[1], in)
		}
	case path == "project/policy":
		var in domain.ProjectPolicyRequest
		if err = decode(r, &in); err == nil {
			result, err = s.Service.UpdateProjectPolicy(ctx, a, key, in)
		}
	case path == "project/publication":
		var in domain.PublicationRequest
		if err = decode(r, &in); err == nil {
			result, err = s.Service.ConfigurePublication(ctx, a, key, in)
		}
	case len(parts) == 3 && parts[0] == "candidates" && parts[2] == "sync":
		var in domain.VersionRequest
		if err = decode(r, &in); err == nil {
			result, err = s.Service.RequestPullRequestSync(ctx, a, key, parts[1], in)
		}
	case path == "project/open":
		var in domain.Project
		if err = decode(r, &in); err == nil {
			result, err = s.Service.CreateDraftProject(ctx, a, key, in)
		}
	case path == "project":
		var in domain.Project
		if err = decode(r, &in); err == nil {
			result, err = s.Service.CreateProject(ctx, a, key, in)
		}
	case path == "requirements":
		var in domain.Requirement
		if err = decode(r, &in); err == nil {
			result, err = s.Service.CreateRequirement(ctx, a, key, in)
		}
	case path == "tasks":
		var in domain.Task
		if err = decode(r, &in); err == nil {
			result, err = s.Service.CreateTask(ctx, a, key, in)
		}
	case path == "pairings":
		var in struct {
			Name string `json:"name"`
		}
		if err = decode(r, &in); err == nil {
			result, err = s.Service.CreatePairing(ctx, a, key, in.Name)
		}
	case path == "project/budget":
		var in domain.BudgetRequest
		if err = decode(r, &in); err == nil {
			result, err = s.Service.ProjectBudget(ctx, a, key, in)
		}
	case len(parts) == 2 && parts[0] == "project":
		var in domain.VersionRequest
		if err = decode(r, &in); err == nil {
			result, err = s.Service.ProjectAction(ctx, a, key, parts[1], in)
		}
	case len(parts) == 3 && parts[0] == "runs" && parts[2] == "confirm-stopped":
		var in domain.StopConfirmation
		if err = decode(r, &in); err == nil {
			result, err = s.Service.ConfirmStopped(ctx, a, key, parts[1], in)
		}
	case len(parts) == 3 && parts[0] == "tasks" && parts[2] == "budget":
		var in domain.BudgetRequest
		if err = decode(r, &in); err == nil {
			result, err = s.Service.TaskBudget(ctx, a, key, parts[1], in)
		}
	case len(parts) == 3 && parts[0] == "tasks" && parts[2] == "scope":
		var in domain.ScopeRequest
		if err = decode(r, &in); err == nil {
			result, err = s.Service.Scope(ctx, a, key, parts[1], in)
		}
	case len(parts) == 3 && parts[0] == "tasks" && parts[2] == "confirm-preparation-stopped":
		var in domain.StopConfirmation
		if err = decode(r, &in); err == nil {
			result, err = s.Service.ConfirmPreparationStopped(ctx, a, key, parts[1], in)
		}
	case len(parts) == 3 && parts[0] == "tasks":
		var in domain.VersionRequest
		if err = decode(r, &in); err == nil {
			result, err = s.Service.TaskAction(ctx, a, key, parts[1], parts[2], in)
		}
	case len(parts) == 3 && parts[0] == "nodes" && parts[2] == "revoke":
		result, err = s.Service.RevokeNode(ctx, a, key, parts[1])
	case len(parts) == 3 && parts[0] == "candidates" && parts[2] == "approve":
		var in domain.VersionRequest
		if err = decode(r, &in); err == nil {
			result, err = s.Service.ApproveCandidate(ctx, a, key, parts[1], in)
		}
	case path == "runner/presence":
		var in domain.ClaimRequest
		if err = decode(r, &in); err == nil {
			err = s.Service.ReportNodePresence(ctx, a, in)
		}
	case path == "runner/claim":
		var in domain.ClaimRequest
		if err = decode(r, &in); err == nil {
			result, err = s.Integration.Claim(ctx, a, key, in)
		}
	case path == "runner/requirements":
		var in domain.RequirementProposal
		if err = decode(r, &in); err == nil {
			result, err = s.Service.SubmitRequirement(ctx, a, key, in)
		}
	case len(parts) == 3 && parts[0] == "runs" && parts[2] == "heartbeat":
		var in domain.HeartbeatRequest
		if err = decode(r, &in); err == nil {
			result, err = s.Service.Heartbeat(ctx, a, parts[1], in)
		}
	case len(parts) == 3 && parts[0] == "runs" && parts[2] == "complete":
		var in domain.CompleteRequest
		if err = decode(r, &in); err == nil {
			result, err = s.Service.Complete(ctx, a, key, parts[1], in)
		}
	case len(parts) == 3 && parts[0] == "runs" && parts[2] == "artifacts":
		result, err = s.Service.UploadArtifact(ctx, a, parts[1], r.Body)
	case len(parts) == 3 && parts[0] == "runs" && parts[2] == "request_scope":
		var in domain.RunScopeRequest
		if err = decode(r, &in); err == nil {
			result, err = s.Service.RequestScope(ctx, a, key, parts[1], in)
		}
	case len(parts) == 3 && parts[0] == "runs" && parts[2] == "block":
		var in struct {
			Generation int64  `json:"generation"`
			Reason     string `json:"reason"`
		}
		if err = decode(r, &in); err == nil {
			result, err = s.Service.Block(ctx, a, key, parts[1], in.Generation, in.Reason)
		}
	case path == "backups":
		if a.Role != "owner" {
			err = domain.Fail("forbidden", "需要用户权限")
		} else {
			var location string
			location, err = s.Integration.Backup(ctx, a)
			result = map[string]string{"path": location}
		}
	default:
		err = domain.ErrNotFound
	}
	respond(w, result, err)
}
func decode(r *http.Request, out any) error {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		return domain.Fail("invalid_input", "JSON请求不能超过1MiB")
	}
	var required []string
	switch out.(type) {
	case *domain.InitializationRequest:
		required = []string{"expected_version", "kind"}
	case *domain.InitializationAnswer:
		required = []string{"expected_version", "question_id", "original_text", "source"}
	case *domain.InitializationConfirm:
		required = []string{"expected_version", "digest"}
	case *domain.ProjectPolicyRequest:
		required = []string{"expected_version", "checks", "protected_paths"}
	}
	if required != nil {
		if e := jsoninput.Required(body, required...); e != nil {
			return domain.Fail("invalid_input", e.Error())
		}
		if e := jsoninput.Decode(body, out, 64<<10); e != nil {
			return domain.Fail("invalid_input", e.Error())
		}
		return nil
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return domain.Fail("invalid_input", "无效JSON: "+err.Error())
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return domain.Fail("invalid_input", "请求必须恰好包含一个JSON对象")
	}
	return nil
}
func respond(w http.ResponseWriter, value any, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err != nil {
		status := 500
		problem := domain.Problem{Code: "internal_error", Message: "中心处理失败，请查看运行日志"}
		var e *domain.Error
		if errors.As(err, &e) {
			problem.Code = e.Code
			problem.Message = e.Message
			switch e.Code {
			case "unauthorized":
				status = 401
			case "forbidden":
				status = 403
			case "invalid_input":
				status = 400
			case "not_found":
				status = 404
			default:
				status = 409
			}
		} else if errors.Is(err, domain.ErrNotFound) {
			status = 404
			problem = domain.Problem{Code: "not_found", Message: "对象不存在"}
		}
		if status == 500 {
			slog.Error("API request failed", "error", err)
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(problem)
		return
	}
	_ = json.NewEncoder(w).Encode(value)
}
