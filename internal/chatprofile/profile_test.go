package chatprofile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPrivateProfileAtomicNoOverwriteAndValidation(t *testing.T) {
	dir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "profile.json")
	p := Profile{CenterURL: "http://127.0.0.1:8080", CredentialID: "client_" + strings.Repeat("a", 32), Token: "chat_" + strings.Repeat("b", 32)}
	raw, _ := json.Marshal(p)
	if e = Write(path, raw); e != nil {
		t.Fatal(e)
	}
	if e = Write(path, raw); e != nil {
		t.Fatal(e)
	}
	if e = Write(path, []byte(`{"different":true}`)); e == nil {
		t.Fatal("overwrote profile")
	}
	loaded, e := Load(path)
	if e != nil || loaded != p {
		t.Fatal(loaded, e)
	}
	if runtime.GOOS != "windows" {
		if e = os.Chmod(path, 0644); e != nil {
			t.Fatal(e)
		}
		if _, e = Load(path); e == nil {
			t.Fatal("public credential file loaded")
		}
		if e = os.Chmod(path, 0600); e != nil {
			t.Fatal(e)
		}
	}
	if e = os.WriteFile(path, []byte(`{"center_url":"http://127.0.0.1","center_url":"https://evil","credential_id":"x","token":"x"}`), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = Load(path); e == nil {
		t.Fatal("duplicate profile accepted")
	}
	link := filepath.Join(dir, "link")
	if e = os.Symlink(path, link); e != nil {
		t.Fatal(e)
	}
	if _, e = Load(link); e == nil {
		t.Fatal("symlink profile loaded")
	}
	if e = Validate(Profile{CenterURL: "http://example.com", CredentialID: p.CredentialID, Token: p.Token}); e == nil {
		t.Fatal("plain remote HTTP")
	}
	if e = Validate(Profile{CenterURL: p.CenterURL, CredentialID: p.CredentialID, Token: "own_" + strings.Repeat("b", 32)}); e == nil {
		t.Fatal("Owner credential loaded")
	}
}
