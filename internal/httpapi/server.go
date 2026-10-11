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
	"strings"

	"coordica/internal/app"
	"coordica/internal/domain"
)

type Server struct {
	Service     *app.Service
	Integration *app.Integrator
	UI          fs.FS
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		respond(w, map[string]string{"status": "ok", "api_version": domain.APIVersion}, nil)
	})
	mux.HandleFunc("POST /api/v1/runner/register", func(w http.ResponseWriter, r *http.Request) {
		var in domain.RegisterRequest
		if e := decode(r, &in); e != nil {
			respond(w, nil, e)
			return
		}
		v, e := s.Service.Register(r.Context(), in)
		respond(w, v, e)
	})
	mux.HandleFunc("/api/v1/", s.auth(s.api))
	if s.UI != nil {
		files := http.FileServer(http.FS(s.UI))
		mux.Handle("/", files)
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Web assets unavailable; build web first", http.StatusServiceUnavailable)
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		mux.ServeHTTP(w, r)
	})
}

type actorKey struct{}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
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
		case path == "state":
			result, err = s.Service.Snapshot(ctx, a)
		case path == "export":
			result, err = s.Service.Export(ctx, a)
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
	case path == "runner/claim":
		var in domain.ClaimRequest
		if err = decode(r, &in); err == nil {
			result, err = s.Integration.Claim(ctx, a, key, in)
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
