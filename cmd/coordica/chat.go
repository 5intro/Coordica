package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"coordica/internal/apiclient"
	"coordica/internal/app"
	"coordica/internal/chatprofile"
	"coordica/internal/domain"
	"coordica/internal/mcp"
	"coordica/internal/runner"
)

func chatInit(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("chat-init", flag.ContinueOnError)
	data := f.String("data", "", "Center data directory (local Owner issuance)")
	runnerData := f.String("runner-data", "", "Existing paired Runner data directory (node-bound issuance)")
	center := f.String("url", "http://127.0.0.1:8080", "Loopback Owner API URL")
	profile := f.String("profile", "", "Absolute shared private profile path")
	name := f.String("name", "", "Shared host interaction name")
	renew := f.Bool("renew", false, "Explicitly revoke and replace an existing host profile")
	if e := f.Parse(args); e != nil {
		return e
	}
	if f.NArg() != 0 || (*data == "") == (*runnerData == "") || *profile == "" || strings.TrimSpace(*name) == "" {
		return errors.New("exactly one of --data or --runner-data, plus --profile and --name required")
	}
	if e := chatprofile.ValidatePath(*profile); e != nil {
		return e
	}
	opts := chatInitOptions{CenterURL: *center, Path: *profile, Name: *name, ChatScope: domain.ChatScopeCenter, Renew: *renew}
	var token string
	if *runnerData != "" {
		// Read the existing registration locally solely to authenticate issuance.
		cfg, e := runner.Load(*runnerData)
		if e != nil {
			return errors.New("cannot load paired Runner configuration")
		}
		opts.CenterURL = cfg.CenterURL
		opts.TLSCA = cfg.TLSCA
		opts.NodeID = cfg.NodeID
		opts.ChatScope = domain.ChatScopeNode
		token = cfg.Token
		if opts.NodeID == "" || token == "" {
			return errors.New("existing paired Runner required; chat-init does not pair nodes")
		}
	} else {
		u, e := url.Parse(*center)
		if e != nil {
			return errors.New("invalid loopback center URL")
		}
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" && u.Scheme != "https" || u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return errors.New("chat-init Owner API must be loopback")
		}
		raw, e := os.ReadFile(filepath.Join(*data, "owner.token"))
		if e != nil {
			return errors.New("cannot read local Owner credential")
		}
		token = strings.TrimSpace(string(raw))
	}
	client, e := apiclient.New(opts.CenterURL, token, opts.TLSCA)
	if e != nil {
		return e
	}
	id, e := initializeHostChatProfile(ctx, client, opts)
	if e != nil {
		return e
	}
	fmt.Printf("Shared coordica interaction credential ready: %s\n", id)
	return nil
}

type chatInitOptions struct {
	CenterURL, Path, Name, TLSCA, ChatScope, NodeID string
	Renew                                           bool
	// Test seam for the limited credential identity check; production always
	// authenticates the saved limited token against the configured server.
	Verify func(context.Context, chatprofile.Profile) (domain.ChatCredentialInfo, error)
}

func verifyChatProfile(ctx context.Context, p chatprofile.Profile) (domain.ChatCredentialInfo, error) {
	var identity domain.ChatCredentialInfo
	client, e := apiclient.New(p.CenterURL, p.Token, p.TLSCA)
	if e != nil {
		return identity, e
	}
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	raw, e := client.Do(ctx, "GET", "/api/v1/chat/identity", "", nil)
	if e != nil {
		return identity, e
	}
	e = json.Unmarshal(raw, &identity)
	return identity, e
}
func initializeChatProfile(ctx context.Context, client mcp.Caller, center, path, name string) (string, error) {
	return initializeHostChatProfile(ctx, client, chatInitOptions{CenterURL: center, Path: path, Name: name, ChatScope: domain.ChatScopeCenter})
}
func initializeHostChatProfile(ctx context.Context, client mcp.Caller, o chatInitOptions) (string, error) {
	release, e := chatprofile.LockInit(o.Path)
	if e != nil {
		return "", e
	}
	defer release()
	issuePath := "/api/v1/chat-credentials"
	if o.ChatScope == domain.ChatScopeNode {
		issuePath = "/api/v1/runner/chat-credentials"
	}
	revoke := func(id string) error {
		_, e := client.Do(context.WithoutCancel(ctx), "POST", issuePath+"/"+id+"/revoke", app.ID("chat_cleanup_"), map[string]any{})
		return e
	}
	if _, e = os.Lstat(o.Path); e == nil {
		p, e := chatprofile.Load(o.Path)
		if e != nil {
			return "", errors.New("existing profile invalid; inspect and remove explicitly before renewal")
		}
		if p.CenterURL != o.CenterURL || p.TLSCA != o.TLSCA {
			return "", errors.New("existing profile belongs to a different center; choose a new path")
		}
		verify := o.Verify
		if verify == nil {
			verify = verifyChatProfile
		}
		identity, verifyErr := verify(ctx, p)
		if verifyErr == nil && (identity.ID != p.CredentialID || identity.ChatScope != o.ChatScope || identity.NodeID != o.NodeID) {
			return "", errors.New("existing shared profile host scope mismatched; choose a new path")
		}
		if !o.Renew {
			if verifyErr != nil || identity.Revoked {
				return "", errors.New("existing profile inactive or unverifiable; use --renew explicitly after checking connection")
			}
			pending, pendingErr := chatprofile.LoadPending(o.Path)
			if pendingErr == nil {
				if pending.CenterURL != o.CenterURL || pending.TLSCA != o.TLSCA || pending.ChatScope != o.ChatScope || pending.NodeID != o.NodeID {
					return "", errors.New("pending grant belongs to another host configuration; resolve original initialization explicitly")
				}
				// Crash after publication can leave a pending identity. Resolve its exact
				// grant before deleting it, so an unrelated inserted profile cannot orphan
				// a previously accepted grant whose ACK was lost.
				raw, replayErr := client.Do(ctx, "POST", issuePath, pending.Key, domain.ChatCredentialRequest{Name: pending.Name})
				var outstanding domain.ChatCredentialGrant
				if replayErr != nil || json.Unmarshal(raw, &outstanding) != nil || !regexp.MustCompile(`^client_[a-f0-9]{32}$`).MatchString(outstanding.Credential.ID) {
					return "", errors.New("pending grant cannot be reconciled; retry original initialization or explicitly renew")
				}
				if outstanding.Credential.ID != p.CredentialID {
					if e = revoke(outstanding.Credential.ID); e != nil {
						return "", errors.New("cannot revoke outstanding own grant; existing shared profile preserved")
					}
				}
			} else if !os.IsNotExist(pendingErr) {
				return "", errors.New("cannot inspect private pending grant; existing shared profile preserved")
			}
			if e = chatprofile.ClearPending(o.Path); e != nil {
				return "", errors.New("cannot clean private pending artifact; shared profile remains active")
			}

			return p.CredentialID, nil
		}
		if e = revoke(p.CredentialID); e != nil {
			return "", errors.New("explicit renewal could not revoke existing own credential")
		}
		if e = os.Remove(o.Path); e != nil {
			return "", errors.New("cannot retire existing private profile")
		}
		if e = chatprofile.ClearPending(o.Path); e != nil {
			return "", e
		}
	} else if !os.IsNotExist(e) {
		return "", errors.New("cannot inspect shared profile")
	}
	pending := chatprofile.Pending{Name: o.Name, CenterURL: o.CenterURL, TLSCA: o.TLSCA, ChatScope: o.ChatScope, NodeID: o.NodeID}
	previous, e := chatprofile.LoadPending(o.Path)
	if e == nil {
		pending.Key = previous.Key
		if pending != previous {
			return "", errors.New("pending host grant differs; retry the original initialization envelope")
		}
	} else if os.IsNotExist(e) {
		pending.Key = app.ID("chat_init_")
		if e = chatprofile.SavePending(o.Path, pending); e != nil {
			return "", errors.New("cannot persist private pending grant")
		}
	} else {
		return "", errors.New("cannot load private pending grant")
	}
	grantRaw, e := client.Do(ctx, "POST", issuePath, pending.Key, domain.ChatCredentialRequest{Name: pending.Name})
	if e != nil {
		var rejected *apiclient.HTTPError
		if errors.As(e, &rejected) && rejected.Problem.Code == "unauthorized" {
			if o.Renew {
				if clearErr := chatprofile.ClearPending(o.Path); clearErr != nil {
					return "", clearErr
				}
				return "", errors.New("inactive pending grant retired explicitly; retry initialization for a new limited credential")
			}
			return "", errors.New("pending grant inactive; use --renew explicitly")
		}
		return "", errors.New("chat credential grant not confirmed; retry identical initialization to recover the pending grant")
	}
	var grant domain.ChatCredentialGrant
	if json.Unmarshal(grantRaw, &grant) != nil || !regexp.MustCompile(`^client_[a-f0-9]{32}$`).MatchString(grant.Credential.ID) {
		return "", errors.New("invalid limited credential response; pending identity retained")
	}
	p := chatprofile.Profile{CenterURL: o.CenterURL, TLSCA: o.TLSCA, CredentialID: grant.Credential.ID, Token: grant.Token}
	if grant.Credential.Revoked || grant.Credential.ChatScope != o.ChatScope || grant.Credential.NodeID != o.NodeID {
		e = errors.New("grant host scope mismatch")
	} else {
		e = chatprofile.Validate(p)
	}
	if e == nil {
		var raw []byte
		raw, e = json.MarshalIndent(p, "", "  ")
		if e == nil {
			e = chatprofile.Write(o.Path, append(raw, '\n'))
		}
	}
	if e != nil {
		if revokeErr := revoke(grant.Credential.ID); revokeErr != nil {
			return "", fmt.Errorf("private profile publication failed; revoke newly issued credential %s through its issuing authority", grant.Credential.ID)
		}
		if clearErr := chatprofile.ClearPending(o.Path); clearErr != nil {
			return "", errors.New("new own chat credential revoked; private pending cleanup failed")
		}
		return "", errors.New("private profile publication failed; newly issued own chat credential revoked")
	}
	if e = chatprofile.ClearPending(o.Path); e != nil {
		if revokeErr := revoke(grant.Credential.ID); revokeErr != nil {
			return "", fmt.Errorf("pending cleanup failed; revoke newly issued credential %s through its issuing authority", grant.Credential.ID)
		}
		// This invocation alone published the profile. Never remove/revoke a reused
		// shared profile when cleanup of an older artifact fails.
		if removeErr := os.Remove(o.Path); removeErr != nil {
			return "", errors.New("new credential revoked after cleanup failure; remove its inactive profile explicitly")
		}
		return "", errors.New("new credential revoked after pending cleanup failure")
	}
	return grant.Credential.ID, nil
}

// Issuance uses the saved paired node solely at the existing issuance endpoint.
// Its token never enters the chat transport, launcher or MCP result.
func provisionNodeInteraction(ctx context.Context, dir, path string) error {
	return provisionInteraction(ctx, dir, path, verifyChatProfile)
}

func provisionInteraction(ctx context.Context, dir, path string, verify func(context.Context, chatprofile.Profile) (domain.ChatCredentialInfo, error)) error {
	// Existing center authority is independent of a co-located Runner's
	// registration. Authenticate only at the profile's own fixed transport;
	// never send this limited token to the Runner's issuance endpoint.
	release, err := chatprofile.LockInit(path)
	if err != nil {
		return err
	}
	if _, err = os.Lstat(path); err == nil {
		p, loadErr := chatprofile.Load(path)
		if loadErr != nil {
			release()
			return errors.New("existing interaction profile invalid; preserved")
		}
		identity, verifyErr := verify(ctx, p)
		if verifyErr != nil || identity.ID != p.CredentialID || identity.Revoked {
			release()
			return errors.New("existing interaction profile inactive or unverifiable; preserved")
		}
		if identity.ChatScope == domain.ChatScopeCenter && identity.NodeID == "" {
			release()
			return nil
		}
		if identity.ChatScope != domain.ChatScopeNode || identity.NodeID == "" {
			release()
			return errors.New("existing interaction profile scope invalid; preserved")
		}
	} else if !os.IsNotExist(err) {
		release()
		return errors.New("cannot inspect existing interaction profile")
	}
	release()
	cfg, err := runner.Load(dir)
	if err != nil || cfg.NodeID == "" || cfg.Token == "" {
		return errors.New("saved paired Runner required")
	}
	client, err := apiclient.New(cfg.CenterURL, cfg.Token, cfg.TLSCA)
	if err != nil {
		return errors.New("cannot connect for limited node issuance")
	}
	defer client.CloseIdleConnections()
	_, err = initializeHostChatProfile(ctx, client, chatInitOptions{CenterURL: cfg.CenterURL, Path: path, Name: "Shared Runner host", TLSCA: cfg.TLSCA, ChatScope: domain.ChatScopeNode, NodeID: cfg.NodeID, Verify: verify})
	return err
}
