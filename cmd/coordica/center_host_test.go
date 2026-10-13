package main

import (
	"context"
	"coordica/internal/localfile"
	"coordica/internal/runner"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"coordica/internal/app"
	"coordica/internal/centerhost"
	"coordica/internal/chatprofile"
	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestOfflineCenterLimitedProvisionReusesIdentityAndRejectsRevokedProfile(t *testing.T) {
	ctx := context.Background()
	root, rootErr := filepath.EvalSymlinks(t.TempDir())
	if rootErr != nil {
		t.Fatal(rootErr)
	}
	db, e := store.Open(filepath.Join(root, "data", "coordica.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := app.New(db, filepath.Join(root, "data"))
	if _, e = s.Bootstrap(ctx); e != nil {
		t.Fatal(e)
	}
	c := centerhost.Config{ConsoleListen: "127.0.0.1:18080", ChatProfile: filepath.Join(root, "profile", "shared.json")}
	if e = provisionCenterInteraction(ctx, s, c); e != nil {
		t.Fatal(e)
	}
	before, e := os.ReadFile(c.ChatProfile)
	if e != nil {
		t.Fatal(e)
	}
	p, e := chatprofile.Load(c.ChatProfile)
	if e != nil {
		t.Fatal(e)
	}
	actor, e := s.Authenticate(ctx, p.Token)
	if e != nil || actor.Role != "chat" || actor.ChatScope != domain.ChatScopeCenter {
		t.Fatal("offline provision exposed Owner scope", actor, e)
	}
	if e = provisionCenterInteraction(ctx, s, c); e != nil {
		t.Fatal(e)
	}
	after, _ := os.ReadFile(c.ChatProfile)
	if string(before) != string(after) {
		t.Fatal("duplicate init replaced shared profile")
	}
	if _, e = s.RevokeChatCredential(ctx, app.Actor{ID: "owner", Role: "owner"}, "revoke", p.CredentialID); e != nil {
		t.Fatal(e)
	}
	if e = provisionCenterInteraction(ctx, s, c); e == nil {
		t.Fatal("revoked profile silently replaced")
	}
	after, _ = os.ReadFile(c.ChatProfile)
	if string(before) != string(after) {
		t.Fatal("revoked profile overwritten")
	}
	var raw map[string]any
	if json.Unmarshal(before, &raw) != nil || raw["owner_token"] != nil {
		t.Fatal("Owner leaked in limited profile")
	}
}

func TestColocatedRunnerPreservesCenterProfileAtItsOwnEndpoint(t *testing.T) {
	ctx := context.Background()
	dir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "chat.json")
	p := chatprofile.Profile{CenterURL: "http://127.0.0.1:18080", CredentialID: "client_" + strings.Repeat("a", 32), Token: "chat_" + strings.Repeat("a", 32)}
	raw, _ := json.Marshal(p)
	if e = localfile.WritePrivate(path, raw); e != nil {
		t.Fatal(e)
	}
	runnerDir := filepath.Join(dir, "runner")
	cfg := runner.Config{CenterURL: "https://alternate.invalid", NodeID: "node_saved", Token: "private-node-token"}
	raw, _ = json.Marshal(cfg)
	if e = localfile.WritePrivate(filepath.Join(runnerDir, "runner.json"), raw); e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(path)
	calls := 0
	verify := func(_ context.Context, got chatprofile.Profile) (domain.ChatCredentialInfo, error) {
		calls++
		if got.CenterURL != p.CenterURL || got.Token != p.Token {
			t.Fatal("center token redirected", got.CenterURL)
		}
		return domain.ChatCredentialInfo{ID: p.CredentialID, ChatScope: domain.ChatScopeCenter}, nil
	}
	if e = provisionInteraction(ctx, runnerDir, path, verify); e != nil {
		t.Fatal(e)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) || calls != 1 {
		t.Fatal("center profile replaced")
	}
	if e = provisionInteraction(ctx, runnerDir, path, func(context.Context, chatprofile.Profile) (domain.ChatCredentialInfo, error) {
		return domain.ChatCredentialInfo{ID: p.CredentialID, ChatScope: domain.ChatScopeCenter, Revoked: true}, nil
	}); e == nil {
		t.Fatal("revoked center profile silently replaced")
	}
}
