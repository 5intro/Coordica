package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"coordica/internal/chatprofile"
	"coordica/internal/domain"
	"coordica/internal/localfile"
)

type chatOnboardAPI struct {
	calls       []string
	keys        []string
	grants      map[string]domain.ChatCredentialGrant
	revoked     map[string]bool
	scope, node string
	lostACK     bool
	onGrant     func()
}

func (f *chatOnboardAPI) Do(_ context.Context, method, path, key string, in any) (json.RawMessage, error) {
	f.calls = append(f.calls, method+" "+path)
	if strings.HasSuffix(path, "/revoke") {
		parts := strings.Split(path, "/")
		if f.revoked == nil {
			f.revoked = map[string]bool{}
		}
		f.revoked[parts[len(parts)-2]] = true
		return []byte(`{}`), nil
	}
	if f.grants == nil {
		f.grants = map[string]domain.ChatCredentialGrant{}
	}
	f.keys = append(f.keys, key)
	grant, ok := f.grants[key]
	if !ok {
		suffix := strings.Repeat("a", 31) + string(rune('a'+len(f.grants)))
		grant = domain.ChatCredentialGrant{Credential: domain.ChatCredentialInfo{ID: "client_" + suffix, ChatScope: f.scope, NodeID: f.node}, Token: "chat_" + suffix}
		f.grants[key] = grant
	}
	if f.onGrant != nil {
		f.onGrant()
		f.onGrant = nil
	}
	if f.lostACK {
		f.lostACK = false
		return nil, errors.New("grant accepted but ACK lost")
	}
	return json.Marshal(grant)
}
func (f *chatOnboardAPI) verify(_ context.Context, p chatprofile.Profile) (domain.ChatCredentialInfo, error) {
	for _, g := range f.grants {
		if g.Token == p.Token && !f.revoked[g.Credential.ID] {
			return g.Credential, nil
		}
	}
	return domain.ChatCredentialInfo{}, errors.New("revoked")
}
func TestHostInitReusesSharedProfileAndPersistsLostGrantIdentity(t *testing.T) {
	for _, scope := range []string{domain.ChatScopeCenter, domain.ChatScopeNode} {
		t.Run(scope, func(t *testing.T) {
			dir, e := filepath.EvalSymlinks(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			api := &chatOnboardAPI{scope: scope, lostACK: true}
			if scope == domain.ChatScopeNode {
				api.node = "node_existing"
			}
			o := chatInitOptions{CenterURL: "http://127.0.0.1:8080", Path: filepath.Join(dir, "host.json"), Name: "Shared host", ChatScope: scope, NodeID: api.node, Verify: api.verify}
			if _, e = initializeHostChatProfile(context.Background(), api, o); e == nil {
				t.Fatal("lost ACK accepted")
			}
			pending, e := chatprofile.LoadPending(o.Path)
			if e != nil || pending.Key == "" {
				t.Fatal("pending grant not private/durable", e)
			}
			if e = localfile.CheckPrivate(o.Path + ".init.pending"); e != nil {
				t.Fatal(e)
			}
			id, e := initializeHostChatProfile(context.Background(), api, o)
			if e != nil {
				t.Fatal(e)
			}
			if len(api.grants) != 1 || api.keys[0] != api.keys[1] || api.keys[1] != pending.Key {
				t.Fatal("grant duplicated after lost ACK/restart")
			}
			p, e := chatprofile.Load(o.Path)
			if e != nil || p.CredentialID != id {
				t.Fatal(e)
			}
			if e = localfile.CheckPrivate(o.Path); e != nil {
				t.Fatal(e)
			}
			calls := len(api.calls)
			o.Name = "Another local agent"
			if reused, e := initializeHostChatProfile(context.Background(), api, o); e != nil || reused != id || len(api.calls) != calls {
				t.Fatal("per-agent grant instead of shared profile", e)
			}
			api.revoked = map[string]bool{id: true}
			if _, e = initializeHostChatProfile(context.Background(), api, o); e == nil || !strings.Contains(e.Error(), "--renew") {
				t.Fatal("revoked profile silently renewed", e)
			}
			o.Renew = true
			if next, e := initializeHostChatProfile(context.Background(), api, o); e != nil || next == id {
				t.Fatal("explicit renewal failed", e)
			}
		})
	}
}
func TestHostInitScopeMismatchAndPublicationFailureRevokeOnlyNewGrant(t *testing.T) {
	dir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "host.json")
	api := &chatOnboardAPI{scope: domain.ChatScopeNode, node: "node_a"}
	o := chatInitOptions{CenterURL: "http://127.0.0.1:8080", Path: path, Name: "Shared", ChatScope: domain.ChatScopeNode, NodeID: "node_b", Verify: api.verify}
	if _, e = initializeHostChatProfile(context.Background(), api, o); e == nil {
		t.Fatal("mismatched node binding accepted")
	}
	if len(api.revoked) != 1 || !strings.HasPrefix(api.calls[len(api.calls)-1], "POST /api/v1/runner/chat-credentials/") {
		t.Fatal("cleanup used wrong authority")
	}
	o.NodeID = "node_a"
	api.onGrant = func() {
		if e := os.Mkdir(path, 0700); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = initializeHostChatProfile(context.Background(), api, o); e == nil {
		t.Fatal("publication overwrite")
	}
	if len(api.revoked) != 2 {
		t.Fatal("orphaned new grant")
	}
}
func TestChatCLIRejectsMixedModesAndRemoteOnboardingBeforeCredentialReads(t *testing.T) {
	ctx := context.Background()
	for _, args := range [][]string{{"--chat-profile", "/missing/profile", "--bootstrap"}, {"--chat-profile", "/missing/profile", "--run", "run"}, {"--chat-profile", "/missing/profile", "--runner-data", "/missing/data"}, {"--chat-profile", "relative"}} {
		if e := serveMCP(ctx, args); e == nil {
			t.Fatal("mixed/invalid profile accepted", args)
		}
	}
	dir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e := chatInit(ctx, []string{"--data", "/missing-owner", "--profile", filepath.Join(dir, "new.json"), "--name", "Chat", "--url", "https://example.com"}); e == nil || !strings.Contains(e.Error(), "loopback") {
		t.Fatal("remote Owner API onboarding", e)
	}
}

func TestPublishedSharedProfileReconcilesCrashPendingWithoutNewGrant(t *testing.T) {
	dir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	api := &chatOnboardAPI{scope: domain.ChatScopeCenter}
	o := chatInitOptions{CenterURL: "http://127.0.0.1:8080", Path: filepath.Join(dir, "host.json"), Name: "Shared", ChatScope: domain.ChatScopeCenter, Verify: api.verify}
	id, e := initializeHostChatProfile(context.Background(), api, o)
	if e != nil {
		t.Fatal(e)
	}
	key := api.keys[0]
	if e = chatprofile.SavePending(o.Path, chatprofile.Pending{Key: key, Name: o.Name, CenterURL: o.CenterURL, ChatScope: o.ChatScope}); e != nil {
		t.Fatal(e)
	}
	if got, e := initializeHostChatProfile(context.Background(), api, o); e != nil || got != id {
		t.Fatal(got, e)
	}
	if len(api.grants) != 1 || api.keys[len(api.keys)-1] != key {
		t.Fatal("crash recovery minted another grant")
	}
	if _, e = chatprofile.LoadPending(o.Path); !os.IsNotExist(e) {
		t.Fatal("pending not cleaned", e)
	}
	// An existing different valid profile does not justify losing the pending
	// key for an accepted grant. Reconcile/revoke only that outstanding grant.
	raw, e := api.Do(context.Background(), "POST", "/api/v1/chat-credentials", "lost-other", domain.ChatCredentialRequest{Name: "Shared"})
	if e != nil {
		t.Fatal(e)
	}
	var other domain.ChatCredentialGrant
	if e = json.Unmarshal(raw, &other); e != nil {
		t.Fatal(e)
	}
	if e = chatprofile.SavePending(o.Path, chatprofile.Pending{Key: "lost-other", Name: o.Name, CenterURL: o.CenterURL, ChatScope: o.ChatScope}); e != nil {
		t.Fatal(e)
	}
	if got, e := initializeHostChatProfile(context.Background(), api, o); e != nil || got != id {
		t.Fatal(got, e)
	}
	if !api.revoked[other.Credential.ID] || api.revoked[id] {
		t.Fatal("wrong credential revoked during pending reconciliation")
	}
}
