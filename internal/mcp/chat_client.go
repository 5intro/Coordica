package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"coordica/internal/apiclient"
	"coordica/internal/chatprofile"
	"coordica/internal/domain"
	"coordica/internal/runner"
)

// ProfileCaller reloads only the startup-fixed protected profile. Missing or unsafe
// files close online capabilities; no other credential is consulted.
type ProfileCaller struct {
	Path                                                string
	ExpectedScope, CenterURL, TLSCA, NodeID, RunnerData string
}

func (c ProfileCaller) Do(ctx context.Context, method, path, key string, input any) (json.RawMessage, error) {
	p, err := chatprofile.Load(c.Path)
	if err != nil {
		return nil, errors.New("limited interaction profile unavailable")
	}
	// Check the trusted destination before any request can carry the token.
	if c.CenterURL != "" && (p.CenterURL != c.CenterURL || p.TLSCA != c.TLSCA) {
		return nil, errors.New("limited profile destination does not match trusted host configuration")
	}
	var nodeID string
	if c.RunnerData != "" {
		cfg, e := runner.Load(c.RunnerData)
		if e != nil || cfg.NodeID == "" || p.CenterURL != cfg.CenterURL || p.TLSCA != cfg.TLSCA || c.ExpectedScope != "" && c.ExpectedScope != domain.ChatScopeNode || c.NodeID != "" && c.NodeID != cfg.NodeID {
			return nil, errors.New("limited profile destination does not match saved Runner registration")
		}
		nodeID = cfg.NodeID
	}
	api, err := apiclient.New(p.CenterURL, p.Token, p.TLSCA)
	if err != nil {
		return nil, errors.New("limited interaction connection unavailable")
	}
	defer api.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// Bind identity validation and the operation to the same loaded profile.
	// The application rechecks persisted scope/revocation in its transaction.
	raw, err := api.Do(ctx, "GET", "/api/v1/chat/identity", "", nil)
	var identity domain.ChatCredentialInfo
	if err != nil || json.Unmarshal(raw, &identity) != nil || identity.ID != p.CredentialID || identity.Revoked || identity.ChatScope != domain.ChatScopeCenter && identity.ChatScope != domain.ChatScopeNode || identity.ChatScope == domain.ChatScopeCenter && identity.NodeID != "" || identity.ChatScope == domain.ChatScopeNode && identity.NodeID == "" {
		return nil, errors.New("limited interaction identity unavailable")
	}
	if c.ExpectedScope != "" && identity.ChatScope != c.ExpectedScope || c.CenterURL != "" && p.CenterURL != c.CenterURL || c.NodeID != "" && identity.NodeID != c.NodeID {
		return nil, errors.New("limited profile does not match trusted host configuration")
	}
	if nodeID != "" && (identity.ChatScope != domain.ChatScopeNode || identity.NodeID != nodeID) {
		return nil, errors.New("limited profile does not match saved Runner registration")
	}
	if method == "GET" && path == "/api/v1/chat/identity" {
		return raw, nil
	}
	return api.Do(ctx, method, path, key, input)
}
