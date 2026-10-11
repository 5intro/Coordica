//go:build windows

package process

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func windowsTestShim(t *testing.T, extension, body string) string {
	t.Helper()
	shim := filepath.Join(t.TempDir(), "installed tool"+extension)
	if !absoluteWindowsDrivePath(shim) || unsafeBatchValue(shim) {
		t.Skip("test host's temporary path is outside restricted shim support")
	}
	if err := os.WriteFile(shim, []byte("@echo off\r\n"+body+"\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return shim
}

func windowsHelperShim(t *testing.T, kind string) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if !absoluteWindowsDrivePath(exe) || unsafeBatchValue(exe) {
		t.Skip("test executable's path is outside restricted shim support")
	}
	// Like the standard npm shim, forward the restricted arguments via %* to
	// a native child. This tests both cmd parsing and the child's argv parsing.
	return windowsTestShim(t, ".cmd", `"`+exe+`" process-helper `+kind+` %*`+"\r\nexit /b %errorlevel%")
}

func TestWindowsBatchStdinAndArguments(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	args := []string{"--profile", "acp", "value with spaces", "开发者", ""}
	shim := windowsHelperShim(t, "stdin")
	s, err := Start(ctx, Spec{Executable: shim, Args: args, Dir: filepath.Dir(shim)})
	if err != nil {
		t.Fatal(err)
	}
	prompt := []byte(strings.Repeat("prompt \"quoted\" & %PATH% ! ^ | < >\n", 100000))
	written := make(chan error, 1)
	go func() {
		writeErr := s.Write(prompt)
		closeErr := s.CloseInput()
		written <- errors.Join(writeErr, closeErr)
	}()
	var output []byte
	for event := range s.Output() {
		if event.Stream == "stdout" {
			output = append(output, event.Line...)
		}
	}
	result, err := s.Wait()
	if writeErr := <-written; err != nil || writeErr != nil || result.ExitCode != 0 || !result.TreeStopped {
		t.Fatalf("result=%+v error=%v write=%v", result, err, writeErr)
	}
	var received struct {
		Args   []string `json:"args"`
		Bytes  int      `json:"bytes"`
		SHA256 string   `json:"sha256"`
	}
	if err = json.Unmarshal(output, &received); err != nil {
		t.Fatal("invalid child result", err)
	}
	if !reflect.DeepEqual(received.Args, args) || received.Bytes != len(prompt) || received.SHA256 != fmt.Sprintf("%x", sha256.Sum256(prompt)) {
		t.Fatalf("stdin/argv corrupted: %+v", received)
	}
}

func TestWindowsBatchExitCode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	shim := windowsTestShim(t, ".bat", "exit /b 23")
	_, result, err := Collect(ctx, Spec{Executable: shim}, 4096)
	if err == nil || result.ExitCode != 23 || !result.TreeStopped {
		t.Fatalf("batch exit lost: %+v %v", result, err)
	}
}

func TestWindowsBatchCancellationStopsChildTree(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	shim := windowsHelperShim(t, "parent-wait")
	marker := filepath.Join(t.TempDir(), "child.marker")
	if unsafeBatchValue(marker) {
		t.Skip("test marker path is outside restricted shim support")
	}
	s, err := Start(ctx, Spec{Executable: shim, Args: []string{marker}, GracePeriod: 100 * time.Millisecond, DrainTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	s.CloseInput()
	ready := false
	for event := range s.Output() {
		if string(event.Line) == "child-ready" {
			ready = true
			cancel()
		}
	}
	result, err := s.Wait()
	if !ready || !errors.Is(err, context.Canceled) || !result.Canceled || !result.TreeStopped {
		t.Fatalf("ready=%v result=%+v error=%v", ready, result, err)
	}
	assertMarkerStopped(t, marker)
}

func TestWindowsBatchDiscoversPATHShimAndRejectsUnsafeInput(t *testing.T) {
	shim := windowsTestShim(t, ".cmd", "exit /b 0")
	t.Setenv("PATH", filepath.Dir(shim)+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
	cmd, err := commandFor(Spec{Executable: "installed tool", Args: []string{"--version"}})
	if err != nil || cmd.SysProcAttr == nil || !strings.Contains(cmd.SysProcAttr.CmdLine, `"`+shim+`"`) {
		t.Fatalf("PATH shim not resolved before wrapping: %+v %v", cmd, err)
	}
	if _, err = Start(context.Background(), Spec{Executable: shim, Args: []string{"unsafe&command"}}); !errors.Is(err, ErrUnsafeBatchCommand) {
		t.Fatal("unsafe batch argument reached process startup", err)
	}
}

func TestWindowsBatchUsesSystemInterpreterAndKeepsSuspension(t *testing.T) {
	t.Setenv("COMSPEC", `C:\untrusted\cmd.exe`)
	shim := windowsTestShim(t, ".cmd", "exit /b 0")
	cmd, err := commandFor(Spec{Executable: shim, Args: []string{"--version"}})
	if err != nil {
		t.Fatal(err)
	}
	system, err := systemCommandInterpreter()
	if err != nil || cmd.Path != system || cmd.Path == os.Getenv("COMSPEC") || len(cmd.Args) != 0 {
		t.Fatalf("interpreter lookup used external configuration: %q %v", cmd.Path, err)
	}
	line := cmd.SysProcAttr.CmdLine
	guard, err := prepare(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.close()
	if cmd.SysProcAttr.CmdLine != line || cmd.SysProcAttr.CreationFlags&0x4 == 0 {
		t.Fatal("Job preparation removed controlled command line or suspended startup")
	}
}

func TestWindowsNativeExecutableDoesNotUseShellRestrictions(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"process-helper", "stdin", `literal&%!^"()`}
	cmd, err := commandFor(Spec{Executable: exe, Args: args})
	if err != nil || cmd.Path != exe || !reflect.DeepEqual(cmd.Args[1:], args) || cmd.SysProcAttr != nil {
		t.Fatalf("native executable wrapped or restricted: %+v %v", cmd, err)
	}
}
