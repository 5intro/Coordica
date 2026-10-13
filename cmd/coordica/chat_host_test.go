package main

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"coordica/internal/app"
	"coordica/internal/chatprofile"
	"coordica/internal/domain"
	"coordica/internal/httpapi"
	"coordica/internal/localfile"
	"coordica/internal/runner"
	"coordica/internal/store"
)

func TestRunnerChatInitReusesExistingRegistrationThroughTLSAndLimitedIdentity(t *testing.T) {
	ctx := context.Background()
	dir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	db, e := store.Open(filepath.Join(dir, "center.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	service := app.New(db, filepath.Join(dir, "center"))
	owner := app.Actor{ID: "owner", Role: "owner"}
	raw, e := service.CreatePairing(ctx, owner, "pair", "Runner")
	if e != nil {
		t.Fatal(e)
	}
	var pair map[string]any
	if e = json.Unmarshal(raw, &pair); e != nil {
		t.Fatal(e)
	}
	registered, e := service.Register(ctx, domain.RegisterRequest{Code: pair["code"].(string), Name: "Existing host", Platform: "test", Capacity: 1})
	if e != nil {
		t.Fatal(e)
	}
	handler := (&httpapi.Server{Service: service}).RunnerHandler()
	var grants, registrations atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/runner/register" {
			registrations.Add(1)
		}
		if r.URL.Path == "/api/v1/runner/chat-credentials" {
			grants.Add(1)
			if r.Header.Get("Authorization") != "Bearer "+registered.Token {
				t.Error("node credential absent from issuance")
			}
		}
		if r.URL.Path == "/api/v1/chat/identity" && r.Header.Get("Authorization") == "Bearer "+registered.Token {
			t.Error("node token used as chat identity")
		}
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	ca := filepath.Join(dir, "public-ca.crt")
	if e = localfile.WritePrivate(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})); e != nil {
		t.Fatal(e)
	}
	runnerDir := filepath.Join(dir, "runner")
	if e = os.Mkdir(runnerDir, 0700); e != nil {
		t.Fatal(e)
	}
	cfg := runner.Config{CenterURL: server.URL, TLSCA: ca, NodeID: registered.NodeID, Token: registered.Token, Name: "Existing", Capacity: 1}
	raw, _ = json.Marshal(cfg)
	configPath := filepath.Join(runnerDir, "runner.json")
	if e = localfile.WritePrivate(configPath, raw); e != nil {
		t.Fatal(e)
	}
	profilePath := filepath.Join(dir, "shared-chat.json")
	args := []string{"--runner-data", runnerDir, "--profile", profilePath, "--name", "Shared host"}
	if e = chatInit(ctx, args); e != nil {
		t.Fatal(e)
	}
	p, e := chatprofile.Load(profilePath)
	if e != nil {
		t.Fatal(e)
	}
	if p.Token == registered.Token || p.TLSCA != ca {
		t.Fatal("node token exported or TLS lost")
	}
	saved, e := os.ReadFile(profilePath)
	if e != nil || strings.Contains(string(saved), registered.Token) {
		t.Fatal("node token leaked")
	}
	before, _ := os.ReadFile(configPath)
	if e = chatInit(ctx, args); e != nil {
		t.Fatal("shared profile not reusable", e)
	}
	after, _ := os.ReadFile(configPath)
	if string(before) != string(after) {
		t.Fatal("registration changed")
	}
	if grants.Load() != 1 || registrations.Load() != 0 {
		t.Fatal("re-pair/per-agent credential issuance", grants.Load(), registrations.Load())
	}
	identity, e := verifyChatProfile(ctx, p)
	if e != nil || identity.ChatScope != domain.ChatScopeNode || identity.NodeID != registered.NodeID {
		t.Fatal(identity, e)
	}
}
func TestSharedChatInitializationSerializesBeforeIssuance(t *testing.T) {
	dir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "shared.json")
	started := make(chan struct{})
	finish := make(chan struct{})
	api := &chatOnboardAPI{scope: domain.ChatScopeCenter, onGrant: func() { close(started); <-finish }}
	o := chatInitOptions{CenterURL: "http://127.0.0.1:8080", Path: path, Name: "Host", ChatScope: domain.ChatScopeCenter, Verify: api.verify}
	done := make(chan error, 1)
	go func() { _, e := initializeHostChatProfile(context.Background(), api, o); done <- e }()
	<-started
	if _, e := initializeHostChatProfile(context.Background(), api, o); e == nil {
		t.Fatal("concurrent initializer got through lock")
	}
	close(finish)
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	if len(api.grants) != 1 {
		t.Fatal("duplicate initialization grants")
	}
}
