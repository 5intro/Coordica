package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const rawHelperEnv = "COORDICA_RAW_PROCESS_TEST_HELPER"

var rawStdout = []byte{'a', 0, 'b', '\r', '\n', '\n', 'c', '\r', 'd'}
var rawStderr = []byte{'e', '\r', '\n', 0, 'f'}

// The subprocess exits directly so the testing package cannot append text to
// the byte streams being checked. It uses no shell or external executable.
func TestRawHelperProcess(t *testing.T) {
	if os.Getenv(rawHelperEnv) != "1" {
		return
	}
	var args []string
	for i, arg := range os.Args {
		if arg == "coordica-raw-helper" {
			args = os.Args[i+1:]
			break
		}
	}
	if len(args) == 0 {
		os.Exit(9)
	}
	write := func(file *os.File, data []byte) {
		if _, err := file.Write(data); err != nil {
			os.Exit(8)
		}
	}
	switch args[0] {
	case "bytes":
		write(os.Stdout, rawStdout)
		write(os.Stderr, rawStderr)
	case "large":
		write(os.Stdout, bytes.Repeat([]byte{'x'}, 5<<20))
	case "both-at-limit":
		write(os.Stdout, bytes.Repeat([]byte{'o'}, 4096))
		write(os.Stderr, bytes.Repeat([]byte{'e'}, 4096))
	case "stdout-limit":
		write(os.Stderr, []byte("other stream"))
		write(os.Stdout, bytes.Repeat([]byte{'o'}, 16<<10))
		time.Sleep(20 * time.Second)
	case "stderr-limit":
		write(os.Stdout, []byte("other stream"))
		write(os.Stderr, bytes.Repeat([]byte{'e'}, 16<<10))
		time.Sleep(20 * time.Second)
	case "child":
		if len(args) != 2 {
			os.Exit(9)
		}
		deadline := time.Now().Add(20 * time.Second)
		for sequence := 0; time.Now().Before(deadline); sequence++ {
			if err := os.WriteFile(args[1], []byte(fmt.Sprint(sequence)), 0600); err != nil {
				os.Exit(8)
			}
			time.Sleep(20 * time.Millisecond)
		}
	case "parent-wait", "parent-exit":
		if len(args) != 2 {
			os.Exit(9)
		}
		child := exec.Command(os.Args[0], rawHelperArgs("child", args[1])...)
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			os.Exit(7)
		}
		if _, err := waitRawMarker(args[1], 5*time.Second); err != nil {
			os.Exit(6)
		}
		write(os.Stdout, []byte{'r', 0, '\r', '\n'})
		if args[0] == "parent-wait" {
			time.Sleep(20 * time.Second)
		}
	default:
		os.Exit(9)
	}
	os.Exit(0)
}

func rawHelperArgs(kind string, args ...string) []string {
	return append([]string{"-test.run=^TestRawHelperProcess$", "--", "coordica-raw-helper", kind}, args...)
}

func rawHelperSpec(t *testing.T, kind string, args ...string) Spec {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Spec{
		Executable:   executable,
		Args:         rawHelperArgs(kind, args...),
		Dir:          t.TempDir(),
		Env:          append(os.Environ(), rawHelperEnv+"=1"),
		GracePeriod:  100 * time.Millisecond,
		DrainTimeout: time.Second,
	}
}

func TestCollectRawPreservesBytes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stdout, stderr, result, err := CollectRaw(ctx, rawHelperSpec(t, "bytes"), 1024, 1024)
	if err != nil || result.ExitCode != 0 || !result.TreeStopped {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if !bytes.Equal(stdout, rawStdout) || !bytes.Equal(stderr, rawStderr) {
		t.Fatalf("raw bytes changed: stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestCollectRawAcceptsLargeUnbrokenOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	spec := rawHelperSpec(t, "large")
	spec.MaxLineBytes = 64
	stdout, stderr, result, err := CollectRaw(ctx, spec, 5<<20, 1024)
	if err != nil || result.ExitCode != 0 || !result.TreeStopped {
		t.Fatalf("result=%+v err=%v stdout=%d", result, err, len(stdout))
	}
	if !bytes.Equal(stdout, bytes.Repeat([]byte{'x'}, 5<<20)) || len(stderr) != 0 {
		t.Fatalf("large output changed: stdout=%d stderr=%q", len(stdout), stderr)
	}
}

func TestCollectRawLimitsAreIndependent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stdout, stderr, result, err := CollectRaw(ctx, rawHelperSpec(t, "both-at-limit"), 4096, 4096)
	if err != nil || result.ExitCode != 0 || !result.TreeStopped {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if !bytes.Equal(stdout, bytes.Repeat([]byte{'o'}, 4096)) || !bytes.Equal(stderr, bytes.Repeat([]byte{'e'}, 4096)) {
		t.Fatalf("independent limits did not preserve output: stdout=%d stderr=%d", len(stdout), len(stderr))
	}
}

func TestCollectRawOverflowStopsManagedProcess(t *testing.T) {
	for _, stream := range []string{"stdout", "stderr"} {
		t.Run(stream, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			stdout, stderr, result, err := CollectRaw(ctx, rawHelperSpec(t, stream+"-limit"), 1024, 1536)
			if !errors.Is(err, ErrOutputLimit) || !result.Canceled || !result.TreeStopped {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if len(stdout) > 1024 || len(stderr) > 1536 {
				t.Fatalf("output exceeded limit: stdout=%d stderr=%d", len(stdout), len(stderr))
			}
			if errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("overflow only stopped at context deadline: %v", err)
			}
		})
	}
}

func TestCollectRawCancellationStopsDescendants(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	marker := filepath.Join(t.TempDir(), "raw-child.marker")
	spec := rawHelperSpec(t, "parent-wait", marker)
	type completion struct {
		result Result
		err    error
	}
	done := make(chan completion, 1)
	go func() {
		_, _, result, err := CollectRaw(ctx, spec, 1024, 1024)
		done <- completion{result, err}
	}()
	_, readyErr := waitRawMarker(marker, 5*time.Second)
	cancel()
	completed := <-done
	if readyErr != nil {
		t.Fatalf("child did not write its marker: %v; result=%+v err=%v", readyErr, completed.result, completed.err)
	}
	if !errors.Is(completed.err, context.Canceled) || !completed.result.Canceled || !completed.result.TreeStopped {
		t.Fatalf("result=%+v err=%v", completed.result, completed.err)
	}
	assertRawMarkerStopped(t, marker)
}

func TestCollectRawNormalExitCleansDescendants(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	marker := filepath.Join(t.TempDir(), "raw-child.marker")
	stdout, stderr, result, err := CollectRaw(ctx, rawHelperSpec(t, "parent-exit", marker), 1024, 1024)
	if err != nil || result.ExitCode != 0 || result.Canceled || !result.TreeStopped {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if !bytes.Equal(stdout, []byte{'r', 0, '\r', '\n'}) || len(stderr) != 0 {
		t.Fatalf("output changed: stdout=%q stderr=%q", stdout, stderr)
	}
	assertRawMarkerStopped(t, marker)
}

func waitRawMarker(path string, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			return data, nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil, errors.New("marker readiness deadline exceeded")
}

func assertRawMarkerStopped(t *testing.T, path string) {
	t.Helper()
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(250 * time.Millisecond)
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) == 0 || !bytes.Equal(first, second) {
		t.Fatalf("descendant marker still changed after cleanup: %q -> %q", first, second)
	}
}
