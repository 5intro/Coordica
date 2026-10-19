//go:build !windows

package github

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPRTokenRejectsPublicPermissionsOnEveryRead(t *testing.T) {
	f := protocol(t)
	if _, e := f.client.token(); e != nil {
		t.Fatal("private token rejected", e)
	}
	if e := os.Chmod(f.client.tokenFile, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e := f.client.token(); e == nil || strings.Contains(e.Error(), f.client.tokenFile) || strings.Contains(e.Error(), "test-token") {
		t.Fatal("public token accepted or credential error leaked private data", e)
	}
}

func TestPRTokenRejectsFIFOWithoutBlocking(t *testing.T) {
	f := protocol(t)
	path := filepath.Join(t.TempDir(), "token-fifo")
	if e := syscall.Mkfifo(path, 0600); e != nil {
		t.Fatal(e)
	}
	f.client.tokenFile = path
	done := make(chan error, 1)
	go func() { _, e := f.client.token(); done <- e }()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("FIFO accepted as a credential")
		}
	case <-time.After(time.Second):
		// Unblock a regressed reader before failing so this test leaves no
		// blocked file read behind, including during a deliberate red run.
		if writer, e := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0600); e == nil {
			writer.Close()
		}
		select {
		case <-done:
		case <-time.After(time.Second):
		}
		t.Fatal("FIFO credential read blocked outside the HTTP timeout")
	}
}
