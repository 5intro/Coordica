package main

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
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
	"coordica/internal/mcp"
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

// Real application issuance/replay without a listener: only the existing
// protocol transport is replaced, not credential authorization or persistence.
type hostGrantCaller struct {
	handler http.Handler
	token   string
	lost    bool
	paths   []string
}

func (c *hostGrantCaller) Do(ctx context.Context, method, path, key string, input any) (json.RawMessage, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	req := httptest.NewRequest(method, path, strings.NewReader(string(raw))).WithContext(ctx)
	req.Host = "127.0.0.1:8080"
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Idempotency-Key", key)
	reply := httptest.NewRecorder()
	c.handler.ServeHTTP(reply, req)
	c.paths = append(c.paths, path)
	if reply.Code != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", reply.Code)
	}
	if c.lost {
		c.lost = false
		return nil, errors.New("accepted grant ACK lost")
	}
	return reply.Body.Bytes(), nil
}
func TestNodeSharedProfileRealAppLostAckReuseAndUnrelatedIdentityConflict(t *testing.T) {
	ctx := context.Background()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "center.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := app.New(db, filepath.Join(dir, "center"))
	owner := app.Actor{ID: "owner", Role: "owner"}
	register := func(key string) domain.RegisterResponse {
		t.Helper()
		raw, err := service.CreatePairing(ctx, owner, key, "Host")
		if err != nil {
			t.Fatal(err)
		}
		var pair map[string]any
		json.Unmarshal(raw, &pair)
		node, err := service.Register(ctx, domain.RegisterRequest{Code: pair["code"].(string), Name: key, Capacity: 1, Platform: "test"})
		if err != nil {
			t.Fatal(err)
		}
		return node
	}
	a, b := register("first"), register("second")
	api := &hostGrantCaller{handler: (&httpapi.Server{Service: service}).RunnerHandler(), token: a.Token, lost: true}
	verify := func(ctx context.Context, p chatprofile.Profile) (domain.ChatCredentialInfo, error) {
		actor, err := service.Authenticate(ctx, p.Token)
		if err != nil {
			return domain.ChatCredentialInfo{}, err
		}
		return service.ChatIdentity(ctx, actor)
	}
	options := chatInitOptions{CenterURL: "http://127.0.0.1:8080", Path: filepath.Join(dir, "interaction.json"), Name: "Shared Runner host", ChatScope: domain.ChatScopeNode, NodeID: a.NodeID, Verify: verify}
	if _, err = initializeHostChatProfile(ctx, api, options); err == nil {
		t.Fatal("lost ACK not surfaced")
	}
	pending, err := chatprofile.LoadPending(options.Path)
	if err != nil {
		t.Fatal(err)
	}
	id, err := initializeHostChatProfile(ctx, api, options)
	if err != nil {
		t.Fatal(err)
	}
	published, err := chatprofile.Load(options.Path)
	if err != nil || published.CredentialID != id || published.Token == a.Token {
		t.Fatal(published.CredentialID, err)
	}
	before, _ := os.ReadFile(options.Path)
	calls := len(api.paths)
	if reused, err := initializeHostChatProfile(ctx, api, options); err != nil || reused != id || len(api.paths) != calls {
		t.Fatal("shared credential not reused", err)
	}
	if pending.Key == "" {
		t.Fatal("lost ACK did not retain replay envelope")
	}
	other := options
	other.NodeID = b.NodeID
	other.Renew = true
	api.token = b.Token
	if _, err = initializeHostChatProfile(ctx, api, other); err == nil {
		t.Fatal("other-node profile replaced")
	}
	after, _ := os.ReadFile(options.Path)
	if string(after) != string(before) {
		t.Fatal("unrelated shared profile changed")
	}
	centerAPI := &hostGrantCaller{handler: (&httpapi.Server{Service: service}).AdminHandler(), token: "unused"}
	other.ChatScope = domain.ChatScopeCenter
	other.NodeID = ""
	if _, err = initializeHostChatProfile(ctx, centerAPI, other); err == nil || len(centerAPI.paths) != 0 {
		t.Fatal("center/node conflict issued or revoked", err)
	}
	for _, path := range api.paths {
		if !strings.HasPrefix(path, "/api/v1/runner/chat-credentials") {
			t.Fatal("issuance re-paired or used chat/Owner authority", path)
		}
	}
	actor, err := service.Authenticate(ctx, published.Token)
	if err != nil {
		t.Fatal("unrelated grant revoked", err)
	}
	identity, err := service.ChatIdentity(ctx, actor)
	if err != nil || identity.NodeID != a.NodeID {
		t.Fatal(identity, err)
	}
}

func TestUnifiedProfileBecomesReadyInLiveProcessAndRevocationClosesCachedCalls(t *testing.T) {
	ctx := context.Background()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "center.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := app.New(db, filepath.Join(dir, "center"))
	owner := app.Actor{ID: "owner", Role: "owner"}
	pair, err := service.CreatePairing(ctx, owner, "pair", "Node")
	if err != nil {
		t.Fatal(err)
	}
	var pairing map[string]any
	json.Unmarshal(pair, &pairing)
	registered, err := service.Register(ctx, domain.RegisterRequest{Code: pairing["code"].(string), Name: "Host", Platform: "test", Capacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer((&httpapi.Server{Service: service}).RunnerHandler())
	defer server.Close()
	ca := filepath.Join(dir, "public-ca.crt")
	if err = localfile.WritePrivate(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})); err != nil {
		t.Fatal(err)
	}
	runnerDir := filepath.Join(dir, "runner")
	cfg := runner.Config{CenterURL: server.URL, TLSCA: ca, NodeID: registered.NodeID, Token: registered.Token, Name: "Host", Capacity: 1}
	raw, _ := json.Marshal(cfg)
	if err = localfile.WritePrivate(filepath.Join(runnerDir, "runner.json"), raw); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "interaction.json")
	input, send := io.Pipe()
	recv, output := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- (mcp.Interaction{Bootstrap: mcp.Bootstrap{DataDir: runnerDir}, Chat: mcp.Chat{API: mcp.ProfileCaller{Path: path}}}).Serve(ctx, input, output)
		output.Close()
	}()
	decoder := json.NewDecoder(recv)
	request := func(line string) map[string]json.RawMessage {
		t.Helper()
		if _, err := io.WriteString(send, line+"\n"); err != nil {
			t.Fatal(err)
		}
		var reply map[string]json.RawMessage
		if err := decoder.Decode(&reply); err != nil {
			t.Fatal(err)
		}
		return reply
	}
	request(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`)
	first := request(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if strings.Contains(string(first["result"]), "submit_requirement") {
		t.Fatal("missing profile advertised chat")
	}
	if err = provisionNodeInteraction(ctx, runnerDir, path); err != nil {
		t.Fatal(err)
	}
	p, err := chatprofile.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	published, _ := os.ReadFile(path)
	if err = provisionNodeInteraction(ctx, runnerDir, path); err != nil {
		t.Fatal("saved registration/profile not reusable", err)
	}
	reused, _ := os.ReadFile(path)
	if string(published) != string(reused) {
		t.Fatal("per-call credential issuance")
	}
	request(`{"jsonrpc":"2.0","id":3,"method":"ping"}`)
	var notification map[string]json.RawMessage
	if err = decoder.Decode(&notification); err != nil || string(notification["method"]) != `"notifications/tools/list_changed"` {
		t.Fatal(notification, err)
	}
	second := request(`{"jsonrpc":"2.0","id":4,"method":"tools/list"}`)
	if !strings.Contains(string(second["result"]), "submit_requirement") || strings.Contains(string(second["result"]), "consult_leader") {
		t.Fatal("wrong authenticated tools", second)
	}
	if _, err = service.RevokeChatCredential(ctx, owner, "revoke", p.CredentialID); err != nil {
		t.Fatal(err)
	}
	rejected := request(`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"get_project_overview"}}`)
	if !strings.Contains(string(rejected["result"]), `"isError":true`) || strings.Contains(string(rejected["result"]), p.Token) || strings.Contains(string(rejected["result"]), registered.Token) {
		t.Fatal(rejected)
	}
	if err = decoder.Decode(&notification); err != nil {
		t.Fatal(err)
	}
	send.Close()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

// Both transports below run the actual HTTP auth/application path. The recorder
// variant also runs in sandboxes that prohibit loopback socket binding.
func TestLocalRunnerRetainsCenterInteractionAndRejectsConflictingProfiles(t *testing.T) {
	for _, live := range []bool{false, true} {
		t.Run(fmt.Sprintf("live_listener_%t", live), func(t *testing.T) {
			ctx := context.Background()
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			db, err := store.Open(filepath.Join(dir, "center.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			service := app.New(db, filepath.Join(dir, "center"))
			owner := app.Actor{ID: "owner", Role: "owner"}
			register := func(key string) domain.RegisterResponse {
				t.Helper()
				raw, err := service.CreatePairing(ctx, owner, key, "host")
				if err != nil {
					t.Fatal(err)
				}
				var pair map[string]any
				json.Unmarshal(raw, &pair)
				cfg, err := service.Register(ctx, domain.RegisterRequest{Code: pair["code"].(string), Name: key, Platform: "test", Capacity: 1})
				if err != nil {
					t.Fatal(err)
				}
				return cfg
			}
			local, other := register("local"), register("other")
			handler := (&httpapi.Server{Service: service}).Handler()
			var identityCalls atomic.Int32
			checked := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/api/v1/chat/identity" {
					t.Error("reuse attempted mutation", r.Method, r.URL.Path)
				}
				if r.Header.Get("Authorization") == "Bearer "+local.Token {
					t.Error("Runner token used for limited identity")
				}
				identityCalls.Add(1)
				handler.ServeHTTP(w, r)
			})
			centerURL := "http://127.0.0.1:8080"
			runnerURL := "https://runner.invalid"
			verify := func(ctx context.Context, p chatprofile.Profile) (domain.ChatCredentialInfo, error) {
				if p.CenterURL != centerURL {
					t.Error("identity transport was replaced")
				}
				api := &hostGrantCaller{handler: checked, token: p.Token}
				raw, err := api.Do(ctx, "GET", "/api/v1/chat/identity", "", nil)
				var identity domain.ChatCredentialInfo
				if err == nil {
					err = json.Unmarshal(raw, &identity)
				}
				return identity, err
			}
			if live {
				server := httptest.NewServer(checked)
				defer server.Close()
				centerURL = server.URL
				runnerServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					t.Error("retained/conflicting credential contacted new Runner URL")
					w.WriteHeader(http.StatusForbidden)
				}))
				defer runnerServer.Close()
				runnerURL = runnerServer.URL
				verify = verifyChatProfile
			}
			cfg := runner.Config{CenterURL: runnerURL, NodeID: local.NodeID, Token: local.Token, Name: "local", Capacity: 1}
			raw, _ := json.Marshal(cfg)
			runnerDir := filepath.Join(dir, "distinct-runner")
			if err := localfile.WritePrivate(filepath.Join(runnerDir, "runner.json"), raw); err != nil {
				t.Fatal(err)
			}
			runnerBefore, _ := os.ReadFile(filepath.Join(runnerDir, "runner.json"))
			for _, kind := range []string{"center", "revoked", "other_node", "corrupt", "wrong_id"} {
				t.Run(kind, func(t *testing.T) {
					var grantRaw json.RawMessage
					if kind == "other_node" {
						actor, err := service.Authenticate(ctx, other.Token)
						if err != nil {
							t.Fatal(err)
						}
						grantRaw, err = service.CreateNodeChatCredential(ctx, actor, "grant-other", domain.ChatCredentialRequest{Name: "other"})
						if err != nil {
							t.Fatal(err)
						}
					} else {
						grantRaw, err = service.CreateChatCredential(ctx, owner, "grant-"+kind, domain.ChatCredentialRequest{Name: kind})
						if err != nil {
							t.Fatal(err)
						}
					}
					var grant domain.ChatCredentialGrant
					json.Unmarshal(grantRaw, &grant)
					p := chatprofile.Profile{CenterURL: centerURL, CredentialID: grant.Credential.ID, Token: grant.Token}
					if kind == "wrong_id" {
						p.CredentialID = "client_" + strings.Repeat("a", 32)
					}
					if kind == "revoked" {
						if _, err := service.RevokeChatCredential(ctx, owner, "revoke", grant.Credential.ID); err != nil {
							t.Fatal(err)
						}
					}
					path := filepath.Join(dir, kind, "interaction.json")
					data, _ := json.Marshal(p)
					if kind == "corrupt" {
						data = []byte(`{"token":"invalid"}`)
					}
					if err := localfile.WritePrivate(path, data); err != nil {
						t.Fatal(err)
					}
					before, _ := os.ReadFile(path)
					selectedRunner := runnerDir
					if kind == "other_node" {
						conflictCfg := cfg
						conflictCfg.CenterURL = centerURL
						selectedRunner = filepath.Join(dir, "conflict-runner")
						configRaw, _ := json.Marshal(conflictCfg)
						if err := localfile.WritePrivate(filepath.Join(selectedRunner, "runner.json"), configRaw); err != nil {
							t.Fatal(err)
						}
					}
					err := provisionInteraction(ctx, selectedRunner, path, verify)
					if (kind == "center") != (err == nil) {
						t.Fatalf("%s reuse error = %v", kind, err)
					}
					if kind == "center" {
						if err := provisionInteraction(ctx, runnerDir, path, verify); err != nil {
							t.Fatal("repeat reuse", err)
						}
					}
					after, _ := os.ReadFile(path)
					runnerAfter, _ := os.ReadFile(filepath.Join(runnerDir, "runner.json"))
					if string(before) != string(after) || string(runnerBefore) != string(runnerAfter) {
						t.Fatal("existing role identity changed")
					}
					if _, err := os.Stat(path + ".init.pending"); !os.IsNotExist(err) {
						t.Fatal("reuse/conflict attempted issuance", err)
					}
				})
			}
			if identityCalls.Load() < 4 {
				t.Fatal("real HTTP identities were not checked")
			}
		})
	}
}
