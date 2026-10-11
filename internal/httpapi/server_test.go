package httpapi

import (
	"bytes"
	"context"
	"coordica/internal/app"
	"coordica/internal/store"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthenticationStrictInputAndNodeBoundary(t *testing.T) {
	dir := t.TempDir()
	db, e := store.Open(filepath.Join(dir, "coordica.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := app.New(db, dir)
	path, e := s.Bootstrap(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	token, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	handler := (&Server{Service: s}).Handler()
	request := func(method, path, auth, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		r.Header.Set("Authorization", "Bearer "+auth)
		r.Header.Set("Idempotency-Key", app.ID("test"))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := request("GET", "/api/v1/state", "invalid", ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := request("POST", "/api/v1/project", strings.TrimSpace(string(token)), `{"unknown":1}`); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("POST", "/api/v1/project", strings.TrimSpace(string(token)), `{} {}`); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	w := request("GET", "/api/v1/state", strings.TrimSpace(string(token)), "")
	if w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	var doc map[string]any
	if json.Unmarshal(w.Body.Bytes(), &doc) != nil || doc["api_version"] != "v1" {
		t.Fatal(w.Body.String())
	}
	if strings.Contains(w.Body.String(), "own_") {
		t.Fatal("credential leaked")
	}
}
