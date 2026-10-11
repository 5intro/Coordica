// Package nodeconsole exposes a Runner's local, restricted Web console. It
// delegates node operations to the center and has no execution or task rules.
package nodeconsole

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"strings"
	"time"

	"coordica/internal/apiclient"
	"coordica/internal/domain"
	"coordica/internal/localhttp"
)

// Client is the center transport needed by the local console. Credentials and
// the configured center URL stay in its implementation, outside the browser.
type Client interface {
	Do(context.Context, string, string, string, any) (json.RawMessage, error)
}

type Server struct {
	API       Client
	UI        fs.FS
	CenterURL string
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/local/info", s.localInfo)
	mux.HandleFunc("GET /api/v1/local/state", s.state)
	mux.HandleFunc("POST /api/v1/local/requirements", s.requirement)
	mux.HandleFunc("/api/", http.NotFound)
	var files http.Handler
	if s.UI != nil {
		files = http.FileServer(http.FS(s.UI))
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		if files == nil {
			problem(w, http.StatusServiceUnavailable, "assets_unavailable", "页面资源未安装")
			return
		}
		files.ServeHTTP(w, r)
	})
	guarded := localhttp.Guard(mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		guarded.ServeHTTP(w, r)
	})
}

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	var snapshot domain.NodeSnapshot
	if s.call(w, r, http.MethodGet, "/api/v1/runner/state", "", nil, &snapshot) {
		if snapshot.APIVersion != domain.APIVersion || snapshot.Node.ID == "" {
			problem(w, http.StatusBadGateway, "invalid_center_response", "中心返回了无效数据，请稍后重试")
			return
		}
		respond(w, http.StatusOK, snapshot)
	}
}

func (s *Server) requirement(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		problem(w, http.StatusUnsupportedMediaType, "invalid_input", "需求提交需要 JSON")
		return
	}
	// The shared guard verifies any supplied Origin. Without Origin, demand a
	// header that a foreign browser page cannot send without CORS preflight.
	if r.Header.Get("Origin") == "" && r.Header.Get("X-Coordica-Local") != "1" {
		problem(w, http.StatusForbidden, "forbidden", "请从本机页面提交需求")
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if strings.TrimSpace(key) == "" || len(key) > 200 {
		problem(w, http.StatusBadRequest, "invalid_input", "写操作需要 Idempotency-Key")
		return
	}
	var input domain.RequirementProposal
	if !decode(w, r, &input) {
		return
	}
	var requirement domain.Requirement
	if s.call(w, r, http.MethodPost, "/api/v1/runner/requirements", key, input, &requirement) {
		if requirement.ID == "" {
			problem(w, http.StatusBadGateway, "invalid_center_response", "中心返回了无效数据，请稍后重试")
			return
		}
		respond(w, http.StatusOK, requirement)
	}
}

func (s *Server) call(w http.ResponseWriter, r *http.Request, method, path, key string, input, output any) bool {
	if s.API == nil {
		problem(w, http.StatusBadGateway, "center_unavailable", "暂时无法连接中心，请稍后重试")
		return false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	data, err := s.API.Do(ctx, method, path, key, input)
	if err != nil {
		upstreamProblem(w, err)
		return false
	}
	// Decode and re-encode declared response fields, never relay unknown fields
	// or a cached registration response that could contain credentials.
	trimmed := bytes.TrimSpace(data)
	if len(data) > 8<<20 || len(trimmed) == 0 || trimmed[0] != '{' || json.Unmarshal(data, output) != nil {
		problem(w, http.StatusBadGateway, "invalid_center_response", "中心返回了无效数据，请稍后重试")
		return false
	}
	return true
}

func upstreamProblem(w http.ResponseWriter, err error) {
	var upstream *apiclient.HTTPError
	if errors.As(err, &upstream) && upstream.Status >= 400 && upstream.Status < 500 {
		code, message := "request_rejected", "中心拒绝了请求，请刷新后重试"
		switch upstream.Problem.Code {
		case "invalid_input":
			code, message = "invalid_input", "需求内容无效，请检查标题和内容"
		case "unauthorized":
			code, message = "unauthorized", "本机节点凭据已失效，请重新配对"
		case "forbidden":
			code, message = "forbidden", "本机节点无权执行此操作，请联系中心管理员"
		case "conflict", "idempotency_conflict", "version_conflict", "incompatible":
			code, message = upstream.Problem.Code, "请求已变化，请刷新后重新提交"
		}
		problem(w, upstream.Status, code, message)
		return
	}
	problem(w, http.StatusBadGateway, "center_unavailable", "暂时无法连接中心，请稍后重试")
}

func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		problem(w, http.StatusBadRequest, "invalid_input", "JSON 请求不能超过 1 MiB")
		return false
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 || body[0] != '{' {
		problem(w, http.StatusBadRequest, "invalid_input", "请求必须恰好包含一个 JSON 对象")
		return false
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		problem(w, http.StatusBadRequest, "invalid_input", "需求 JSON 无效或包含不支持的字段")
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		problem(w, http.StatusBadRequest, "invalid_input", "请求必须恰好包含一个 JSON 对象")
		return false
	}
	return true
}

func problem(w http.ResponseWriter, status int, code, message string) {
	respond(w, status, domain.Problem{Code: code, Message: message})
}

func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
