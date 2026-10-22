package process

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 2 && os.Args[1] == "process-helper" {
		switch os.Args[2] {
		case "duplex":
			os.Exit(duplexInputHelper())
		case "stdin":
			input, err := io.ReadAll(os.Stdin)
			if err != nil {
				os.Exit(8)
			}
			json.NewEncoder(os.Stdout).Encode(struct {
				Args   []string `json:"args"`
				Bytes  int      `json:"bytes"`
				SHA256 string   `json:"sha256"`
			}{os.Args[3:], len(input), fmt.Sprintf("%x", sha256.Sum256(input))})
			os.Exit(0)
		case "stream":
			for i := 0; i < 3000; i++ {
				fmt.Printf("{\"sequence\":%d}\n", i)
			}
			fmt.Fprintln(os.Stderr, "diagnostic")
			os.Exit(0)
		case "child":
			for i := 0; i < 10000; i++ {
				os.WriteFile(os.Args[3], []byte(fmt.Sprint(i)), 0600)
				time.Sleep(10 * time.Millisecond)
			}
			os.Exit(0)
		case "parent-exit", "parent-wait":
			child := exec.Command(os.Args[0], "process-helper", "child", os.Args[3])
			child.Stdout, child.Stderr = os.Stdout, os.Stderr
			if err := child.Start(); err != nil {
				os.Exit(5)
			}
			deadline := time.Now().Add(3 * time.Second)
			for {
				if _, err := os.Stat(os.Args[3]); err == nil {
					break
				}
				if time.Now().After(deadline) {
					os.Exit(6)
				}
				time.Sleep(10 * time.Millisecond)
			}
			fmt.Println("child-ready")
			if os.Args[2] == "parent-wait" {
				time.Sleep(time.Minute)
			}
			os.Exit(0)
		case "long-line":
			fmt.Println(strings.Repeat("a", 10000))
			os.Exit(0)
		}
		os.Exit(7)
	}
	os.Exit(m.Run())
}

func helperSpec(t *testing.T, kind string, args ...string) Spec {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Spec{Executable: exe, Args: append([]string{"process-helper", kind}, args...), Dir: t.TempDir(), GracePeriod: 100 * time.Millisecond, DrainTimeout: time.Second}
}

func TestStructuredOutputIsDrained(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := Start(ctx, helperSpec(t, "stream"))
	if err != nil {
		t.Fatal(err)
	}
	s.CloseInput()
	stdout, stderr := 0, 0
	for event := range s.Output() {
		if event.Stream == "stdout" {
			stdout++
		} else {
			stderr++
		}
	}
	r, err := s.Wait()
	if err != nil || r.ExitCode != 0 || !r.TreeStopped || stdout != 3000 || stderr != 1 {
		t.Fatalf("result=%+v err=%v output=%d/%d", r, err, stdout, stderr)
	}
}

func TestCancellationStopsDescendants(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	marker := filepath.Join(t.TempDir(), "child.marker")
	s, err := Start(ctx, helperSpec(t, "parent-wait", marker))
	if err != nil {
		t.Fatal(err)
	}
	ready := false
	for event := range s.Output() {
		if string(event.Line) == "child-ready" {
			ready = true
			cancel()
		}
	}
	r, err := s.Wait()
	if !ready || !errors.Is(err, context.Canceled) || !r.TreeStopped {
		t.Fatalf("ready=%v result=%+v err=%v", ready, r, err)
	}
	assertMarkerStopped(t, marker)
}

func TestSuccessfulParentCannotLeavePipeHoldingChild(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	marker := filepath.Join(t.TempDir(), "child.marker")
	s, err := Start(ctx, helperSpec(t, "parent-exit", marker))
	if err != nil {
		t.Fatal(err)
	}
	for range s.Output() {
	}
	r, err := s.Wait()
	if err != nil || r.ExitCode != 0 || !r.TreeStopped {
		t.Fatalf("result=%+v err=%v", r, err)
	}
	assertMarkerStopped(t, marker)
}

func assertMarkerStopped(t *testing.T, marker string) {
	t.Helper()
	first, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	second, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("descendant still active: %q -> %q", first, second)
	}
}

func TestOversizedLineIsExplicitFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	spec := helperSpec(t, "long-line")
	spec.MaxLineBytes = 1024
	s, err := Start(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	for range s.Output() {
	}
	_, err = s.Wait()
	if err == nil {
		t.Fatal("oversized protocol line silently accepted")
	}
}
