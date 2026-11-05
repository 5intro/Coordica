package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"coordica/internal/apiclient"
	"coordica/internal/app"
	"coordica/internal/domain"
	"coordica/internal/httpapi"
	"coordica/internal/store"
)

const ctlPolicyFields = `"checks":[{"name":"check","command":"git","args":[],"timeout_seconds":1}],"protected_paths":["notes/**"]`

func TestCtlJSONDuplicateFieldsMatchDirectHTTP(t *testing.T) {
	cases := []struct{ name, body string }{
		{"top-level", `{"expected_version":0,"expected_version":1,` + ctlPolicyFields + `}`},
		{"escaped-top-level", `{"expected_version":0,"expected_vers\u0069on":1,` + ctlPolicyFields + `}`},
		{"nested-in-array", `{"expected_version":1,"checks":[{"name":"first","name":"check","command":"git","args":[],"timeout_seconds":1}],"protected_paths":["notes/**"]}`},
		{"escaped-nested", `{"expected_version":1,"checks":[{"name":"first","na\u006de":"check","command":"git","args":[],"timeout_seconds":1}],"protected_paths":["notes/**"]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			direct := newCtlJSONFixture(t, 1)
			status, response := direct.post(t, "project/policy", tc.body)
			var problem domain.Problem
			if status != http.StatusBadRequest || json.Unmarshal(response, &problem) != nil || problem.Code != "invalid_input" {
				t.Fatalf("direct HTTP did not reject duplicate fields: status=%d body=%s", status, response)
			}
			for _, source := range []string{"file", "stdin"} {
				t.Run(source, func(t *testing.T) {
					f := newCtlJSONFixture(t, 1)
					_, err := f.ctl(t, http.MethodPost, "project/policy", source, tc.body)
					var rejected *apiclient.HTTPError
					if !errors.As(err, &rejected) || rejected.Status != status || rejected.Problem != problem {
						t.Errorf("ctl must preserve duplicate fields for the HTTP decoder: error=%v, want HTTP %d %+v", err, status, problem)
					}
					project, err := f.service.Project(context.Background())
					if err != nil || !reflect.DeepEqual(project, f.project) {
						t.Errorf("rejected duplicate fields changed the temporary project: project=%+v error=%v", project, err)
					}
				})
			}
		})
	}
}

func TestCtlJSONLargeIntegerMatchesDirectHTTP(t *testing.T) {
	// These are legal int64 versions, including one near the upper boundary.
	// Neither can survive an intermediate float64 without changing its value.
	for _, version := range []int64{9007199254740993, 9223372036854775806} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			body := fmt.Sprintf(`{"expected_version":%d,%s}`, version, ctlPolicyFields)
			direct := newCtlJSONFixture(t, version)
			status, response := direct.post(t, "project/policy", body)
			want := ctlJSONProject(t, response)
			if status != http.StatusOK || want.Version != version+1 {
				t.Fatalf("direct HTTP did not accept the exact version: status=%d body=%s", status, response)
			}
			for _, source := range []string{"file", "stdin"} {
				t.Run(source, func(t *testing.T) {
					f := newCtlJSONFixture(t, version)
					response, err := f.ctl(t, http.MethodPost, "project/policy", source, body)
					if err != nil {
						t.Fatalf("ctl changed a legal integer accepted by direct HTTP: %v", err)
					}
					if got := ctlJSONProject(t, response); !reflect.DeepEqual(got, want) {
						t.Fatalf("ctl result differs from direct HTTP: got=%+v want=%+v", got, want)
					}
					persisted, err := f.service.Project(context.Background())
					if err != nil || !reflect.DeepEqual(persisted, want) {
						t.Fatalf("exact version was not persisted: project=%+v error=%v", persisted, err)
					}
				})
			}
		})
	}
}

func TestCtlJSONValidPolicyMatchesDirectHTTP(t *testing.T) {
	body := ` { "expected_version": 1, "checks": [{"name":"check \u4e2d","command":"git","args":["status","--short"],"timeout_seconds":1}], "protected_paths": ["notes/**"] } `
	direct := newCtlJSONFixture(t, 1)
	status, response := direct.post(t, "project/policy", body)
	if status != http.StatusOK {
		t.Fatalf("valid direct HTTP input failed: status=%d body=%s", status, response)
	}
	want := ctlJSONProject(t, response)
	for _, source := range []string{"file", "stdin"} {
		t.Run(source, func(t *testing.T) {
			f := newCtlJSONFixture(t, 1)
			response, err := f.ctl(t, http.MethodPost, "project/policy", source, body)
			if err != nil {
				t.Fatal(err)
			}
			if got := ctlJSONProject(t, response); !reflect.DeepEqual(got, want) {
				t.Fatalf("valid ctl fields differ from direct HTTP: got=%+v want=%+v", got, want)
			}
		})
	}
}

func TestCtlJSONTransportDefaultsAndShapes(t *testing.T) {
	// GET state ignores its body, so all legal JSON shapes can be observed at
	// the actual HTTP boundary without imposing an endpoint's object schema.
	for _, tc := range []struct{ name, body string }{
		{"object", `{"flag":false,"items":[]}`},
		{"array", `["text",false,{"nested":[]}]`},
		{"null", `null`},
		{"string", `"text"`},
		{"number", `1.25`},
		{"boolean", `true`},
	} {
		for _, source := range []string{"file", "stdin"} {
			t.Run(tc.name+"/"+source, func(t *testing.T) {
				f := newCtlJSONFixture(t, 1)
				if _, err := f.ctl(t, http.MethodGet, "state", source, tc.body); err != nil {
					t.Fatal(err)
				}
				if got := string(<-f.received); got != tc.body {
					t.Fatalf("ctl changed the JSON shape: body=%q want=%q", got, tc.body)
				}
			})
		}
	}
	t.Run("omitted-body-default", func(t *testing.T) {
		f := newCtlJSONFixture(t, 1)
		if _, err := f.ctl(t, http.MethodGet, "state", "", ""); err != nil {
			t.Fatal(err)
		}
		if got := string(<-f.received); got != "{}" {
			t.Fatalf("omitted --body-file sent %q, want {}", got)
		}
	})
}

type ctlJSONFixture struct {
	service   *app.Service
	project   domain.Project
	server    *httptest.Server
	tokenFile string
	owner     string
	received  chan []byte
}

func newCtlJSONFixture(t *testing.T, version int64) *ctlJSONFixture {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "coordica.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s := app.New(db, dir)
	s.Now = func() time.Time { return time.Unix(1700000000, 0).UTC() }
	ctx := context.Background()
	tokenFile, err := s.Bootstrap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	token, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	owner := strings.TrimSpace(string(token))
	actor, err := s.Authenticate(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateProject(ctx, actor, "fixture-project", domain.Project{
		Name: "JSON transport", RepositoryURL: "https://example.invalid/fixture", TargetBranch: "main",
		Checks: []domain.Check{{Name: "check", Command: "git", Args: []string{}, TimeoutSeconds: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	project, err := s.Project(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Seed only this isolated fixture's version to exercise the full int64 API.
	project.Version = version
	if err := db.Update(ctx, func(tx *store.Tx) error { return tx.Put("project", project.ID, project) }); err != nil {
		t.Fatal(err)
	}
	f := &ctlJSONFixture{service: s, project: project, tokenFile: tokenFile, owner: owner, received: make(chan []byte, 1)}
	handler := (&httpapi.Server{Service: s}).AdminHandler()
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "reading test request", http.StatusBadRequest)
			return
		}
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(body))
		f.received <- body
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *ctlJSONFixture) post(t *testing.T, path, body string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, f.server.URL+"/api/v1/"+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+f.owner)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "direct-json")
	res, err := f.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	response, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, response
}

func (f *ctlJSONFixture) ctl(t *testing.T, method, path, source, body string) ([]byte, error) {
	t.Helper()
	dir := t.TempDir()
	output, err := os.Create(filepath.Join(dir, "stdout.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	previousIn, previousOut := os.Stdin, os.Stdout
	defer func() { os.Stdin, os.Stdout = previousIn, previousOut }()
	os.Stdout = output
	args := []string{"--center", f.server.URL, "--token-file", f.tokenFile, "--method", method, "--path", path, "--key", "ctl-json"}
	if source != "" {
		inputFile := filepath.Join(dir, "input.json")
		if err := os.WriteFile(inputFile, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		bodyFile := inputFile
		if source == "stdin" {
			input, err := os.Open(inputFile)
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			os.Stdin, bodyFile = input, "-"
		}
		args = append(args, "--body-file", bodyFile)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	callErr := ctl(ctx, args)
	response, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	return response, callErr
}

func ctlJSONProject(t *testing.T, raw []byte) domain.Project {
	t.Helper()
	var project domain.Project
	if err := json.Unmarshal(raw, &project); err != nil {
		t.Fatalf("decode project response %s: %v", raw, err)
	}
	return project
}
