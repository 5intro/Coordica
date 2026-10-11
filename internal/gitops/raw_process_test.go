package gitops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The test binary substitutes for Git so cancellation exercises the production
// invocation and its real descendants, including Windows Job inheritance.
func TestMain(m *testing.M) {
	mode := os.Getenv("COORDICA_GIT_RAW_TEST_MODE")
	if mode == "" {
		os.Exit(m.Run())
	}
	marker := os.Getenv("COORDICA_GIT_RAW_TEST_MARKER")
	switch mode {
	case "snapshot-index-failure", "snapshot-index-cancel":
		if rawGitHelperArgument("diff-index") >= 0 {
			if mode == "snapshot-index-cancel" {
				os.WriteFile(marker, []byte("diff-index-ready"), 0600)
				time.Sleep(20 * time.Second)
			}
			fmt.Fprint(os.Stderr, "diff-index diagnostic")
			os.Exit(23)
		}
	case "bundle-cleanup-failure", "bundle-cancel-and-cleanup-hang":
		if index := rawGitHelperArgument("bundle"); index >= 0 && len(os.Args) > index+2 && os.Args[index+1] == "create" {
			if mode == "bundle-cancel-and-cleanup-hang" {
				os.WriteFile(marker, []byte("bundle-create-ready"), 0600)
				time.Sleep(20 * time.Second)
			}
			if err := os.WriteFile(os.Args[index+2], []byte("bundle material\x00without final newline"), 0600); err != nil {
				os.Exit(95)
			}
		}
		if rawGitHelperArgument("update-ref") >= 0 && rawGitHelperArgument("-d") >= 0 {
			if mode == "bundle-cancel-and-cleanup-hang" {
				os.WriteFile(marker+".cleanup", []byte("cleanup-ready"), 0600)
				time.Sleep(20 * time.Second)
			}
			fmt.Fprint(os.Stderr, "bundle cleanup diagnostic")
			os.Exit(23)
		}
	case "child":
		for i := 0; i < 10000; i++ {
			if err := os.WriteFile(marker, []byte(fmt.Sprint(i)), 0600); err != nil {
				os.Exit(91)
			}
			time.Sleep(10 * time.Millisecond)
		}
	case "parent-wait", "parent-exit":
		os.Setenv("COORDICA_GIT_RAW_TEST_MODE", "child")
		child := exec.Command(os.Args[0])
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			os.Exit(92)
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := os.Stat(marker); err == nil {
				break
			}
			if time.Now().After(deadline) {
				os.Exit(93)
			}
			time.Sleep(10 * time.Millisecond)
		}
		fmt.Print("child-ready\x00")
		if mode == "parent-wait" {
			time.Sleep(time.Minute)
		}
	case "stderr-overflow":
		fmt.Fprint(os.Stderr, strings.Repeat("e", (64<<10)+10000))
	case "failure":
		fmt.Print("partial\x00output")
		fmt.Fprint(os.Stderr, "diagnostic without final newline")
		os.Exit(23)
	default:
		os.Exit(94)
	}
	os.Exit(0)
}

func rawGitHelperArgument(name string) int {
	for index, argument := range os.Args {
		if argument == name {
			return index
		}
	}
	return -1
}

func TestGitRawOutputPreservesBinaryAndNULTerminatedRecords(t *testing.T) {
	g := gitClient(t)
	repo, _ := newRepo(t, g)
	// The binary blob also contains a line larger than Scanner's former limit.
	data := append([]byte{0, 255, '\r', '\n'}, bytes.Repeat([]byte{'a'}, (4<<20)+100)...)
	data = append(data, 0, 'z')
	if err := os.WriteFile(filepath.Join(repo, "src", "raw.bin"), data, 0600); err != nil {
		t.Fatal(err)
	}
	commit(t, g, repo, "raw binary output")
	got := mustRun(t, g, repo, "show", "HEAD:src/raw.bin")
	if !bytes.Equal(got, data) {
		t.Fatalf("Git blob bytes changed: got %d, want %d", len(got), len(data))
	}
	got = mustRun(t, g, repo, "ls-tree", "-z", "--name-only", "HEAD", "src")
	if !bytes.Equal(got, []byte("src\x00")) {
		t.Fatalf("NUL record changed: %q", got)
	}
}

func TestGitManagedFailurePreservesPartialBytesAndExitError(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	got, err := New(exe).run(context.Background(), "", []string{"COORDICA_GIT_RAW_TEST_MODE=failure"}, "version")
	var failure *Error
	var exit *exec.ExitError
	if !bytes.Equal(got, []byte("partial\x00output")) || !errors.Is(err, ErrGit) || !errors.As(err, &failure) || failure.ExitCode != 23 || failure.Detail != "diagnostic without final newline" || !errors.As(err, &exit) || exit.ExitCode() != 23 {
		t.Fatalf("lost managed failure evidence: output=%q failure=%+v err=%v", got, failure, err)
	}
}

func TestGitManagedStderrLimitIsExplicit(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(exe).run(context.Background(), "", []string{"COORDICA_GIT_RAW_TEST_MODE=stderr-overflow"}, "version")
	if !errors.Is(err, ErrOutputLimit) {
		t.Fatalf("oversize stderr did not fail explicitly: %v", err)
	}
}

func TestGitRefClassificationPreservesManagedFailureChain(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("COORDICA_GIT_RAW_TEST_MODE", "failure")
	err = New(exe).validRef(context.Background(), "refs/heads/main")
	var exit *exec.ExitError
	if !errors.Is(err, ErrInvalidRef) || !errors.Is(err, ErrGit) || !errors.As(err, &exit) || exit.ExitCode() != 23 {
		t.Fatalf("Git classification discarded managed error evidence: %v", err)
	}
}

func TestSnapshotIndexFailurePreservesDirtyAndManagedCause(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("COORDICA_GIT_RAW_TEST_MODE", "snapshot-index-failure")
	err = New(exe).snapshotIndex(context.Background(), "", OID(strings.Repeat("a", 40)))
	var exit *exec.ExitError
	if !errors.Is(err, ErrDirty) || !errors.Is(err, ErrGit) || !errors.As(err, &exit) || exit.ExitCode() != 23 {
		t.Fatalf("snapshot index discarded managed error: %v", err)
	}
}

func TestSnapshotIndexCancellationPreservesDirtyAndContext(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "diff-index.marker")
	t.Setenv("COORDICA_GIT_RAW_TEST_MODE", "snapshot-index-cancel")
	t.Setenv("COORDICA_GIT_RAW_TEST_MARKER", marker)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- New(exe).snapshotIndex(ctx, "", OID(strings.Repeat("a", 40))) }()
	readyErr := waitRawGitMarker(marker, 5*time.Second)
	cancel()
	err = <-done
	if readyErr != nil || !errors.Is(err, ErrDirty) || !errors.Is(err, ErrGit) || !errors.Is(err, context.Canceled) {
		t.Fatalf("snapshot index discarded cancellation: ready=%v err=%v", readyErr, err)
	}
}

func TestBundleReferenceCleanupFailureIsReturnedWithPrimaryFailure(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprint(existing), func(t *testing.T) {
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("COORDICA_GIT_RAW_TEST_MODE", "bundle-cleanup-failure")
			dest := filepath.Join(t.TempDir(), "material.bundle")
			if existing {
				if err := os.WriteFile(dest, []byte("existing material"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			result, err := New(exe).createBundle(context.Background(), "", OID(strings.Repeat("a", 40)), OID(strings.Repeat("b", 40)), dest)
			var exit *exec.ExitError
			if !errors.Is(err, ErrGit) || !errors.As(err, &exit) || exit.ExitCode() != 23 || !strings.Contains(err.Error(), "bundle reference cleanup") {
				t.Fatalf("cleanup failure was lost: result=%+v err=%v", result, err)
			}
			if existing && !errors.Is(err, os.ErrExist) {
				t.Fatalf("cleanup replaced primary copy error: %v", err)
			}
		})
	}
}

func TestBundleReferenceCleanupAfterCancellationHasFiniteDeadline(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "bundle.marker")
	t.Setenv("COORDICA_GIT_RAW_TEST_MODE", "bundle-cancel-and-cleanup-hang")
	t.Setenv("COORDICA_GIT_RAW_TEST_MARKER", marker)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		_, bundleErr := New(exe).createBundle(ctx, "", OID(strings.Repeat("a", 40)), OID(strings.Repeat("b", 40)), filepath.Join(filepath.Dir(marker), "material.bundle"))
		done <- bundleErr
	}()
	readyErr := waitRawGitMarker(marker, 5*time.Second)
	cancel()
	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("reference cleanup exceeded its finite deadline")
	}
	_, cleanupErr := os.Stat(marker + ".cleanup")
	if readyErr != nil || cleanupErr != nil || !errors.Is(err, context.Canceled) || !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, ErrGit) {
		t.Fatalf("cleanup did not preserve bounded shutdown and primary cancellation: elapsed=%s ready=%v cleanup=%v err=%v", time.Since(start), readyErr, cleanupErr, err)
	}
}

func waitRawGitMarker(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return errors.New("Git helper marker deadline exceeded")
}

func TestGitManagedCancellationAndSuccessfulExitStopDescendants(t *testing.T) {
	for _, mode := range []string{"parent-wait", "parent-exit"} {
		t.Run(mode, func(t *testing.T) {
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(t.TempDir(), "ordinary-child.marker")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			type outcome struct {
				output []byte
				err    error
			}
			done := make(chan outcome, 1)
			go func() {
				output, runErr := New(exe).run(ctx, "", []string{"COORDICA_GIT_RAW_TEST_MODE=" + mode, "COORDICA_GIT_RAW_TEST_MARKER=" + marker}, "clone")
				done <- outcome{output, runErr}
			}()
			deadline := time.Now().Add(5 * time.Second)
			for {
				if _, err := os.Stat(marker); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("managed Git child never became active")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if mode == "parent-wait" {
				cancel()
			}
			result := <-done
			if mode == "parent-wait" && !errors.Is(result.err, context.Canceled) || mode == "parent-exit" && result.err != nil {
				t.Fatalf("unexpected Git termination: output=%q err=%v", result.output, result.err)
			}
			if mode == "parent-exit" && !bytes.Equal(result.output, []byte("child-ready\x00")) {
				t.Fatalf("parent output lost its NUL: %q", result.output)
			}
			first, err := os.ReadFile(marker)
			if err != nil {
				t.Fatal(err)
			}
			time.Sleep(150 * time.Millisecond)
			second, err := os.ReadFile(marker)
			if err != nil || !bytes.Equal(first, second) {
				t.Fatalf("Git descendant kept changing its marker: %q -> %q (%v)", first, second, err)
			}
		})
	}
}
