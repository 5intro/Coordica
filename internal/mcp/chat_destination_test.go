package mcp

import (
	"context"
	"coordica/internal/chatprofile"
	"coordica/internal/domain"
	"coordica/internal/localfile"
	"coordica/internal/runner"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestProfileDestinationBindingPrecedesAnyCredentialRequest(t *testing.T) {
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(root, "chat.json")
	dir := filepath.Join(root, "runner")
	p := chatprofile.Profile{CenterURL: "http://127.0.0.1:1", TLSCA: "/does-not-exist-ca", CredentialID: "client_" + strings.Repeat("a", 32), Token: "chat_" + strings.Repeat("a", 32)}
	write := func() {
		b, _ := json.Marshal(p)
		if e := localfile.WritePrivate(path, b); e != nil {
			t.Fatal(e)
		}
	}
	write()
	cfg := runner.Config{CenterURL: "http://127.0.0.1:2", NodeID: "node_saved", Token: "private", TLSCA: "/saved-ca"}
	b, _ := json.Marshal(cfg)
	if e = localfile.WritePrivate(filepath.Join(dir, "runner.json"), b); e != nil {
		t.Fatal(e)
	}
	for _, c := range []ProfileCaller{{Path: path, ExpectedScope: domain.ChatScopeCenter, CenterURL: "http://127.0.0.1:2"}, {Path: path, ExpectedScope: domain.ChatScopeCenter, CenterURL: p.CenterURL, TLSCA: "/saved-ca"}, {Path: path, ExpectedScope: domain.ChatScopeNode, RunnerData: dir}, {Path: path, ExpectedScope: domain.ChatScopeNode, RunnerData: filepath.Join(root, "missing")}} {
		_, e := c.Do(context.Background(), "GET", "/api/v1/chat/identity", "", nil)
		if e == nil || !strings.Contains(e.Error(), "destination") {
			t.Fatal("client construction or network preceded binding", e)
		}
	}
	_ = os.Remove(path)
}

// Real listeners remain mandatory in the independent root suite. A tampered
// profile must cause zero authenticated requests at the supplied alternative.
func TestMismatchingProfileNeverTransmitsToAlternativeListener(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		t.Error("token sent before destination binding")
		w.WriteHeader(401)
	}))
	defer server.Close()
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(root, "chat.json")
	p := chatprofile.Profile{CenterURL: server.URL, CredentialID: "client_" + strings.Repeat("a", 32), Token: "chat_" + strings.Repeat("a", 32)}
	b, _ := json.Marshal(p)
	if e = localfile.WritePrivate(path, b); e != nil {
		t.Fatal(e)
	}
	c := ProfileCaller{Path: path, ExpectedScope: domain.ChatScopeCenter, CenterURL: "http://127.0.0.1:2"}
	if _, e = c.Do(context.Background(), "GET", "/api/v1/chat/identity", "", nil); e == nil || calls.Load() != 0 {
		t.Fatal("alternate received credential", e, calls.Load())
	}
}
