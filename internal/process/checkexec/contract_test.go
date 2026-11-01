package checkexec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"coordica/internal/domain"
)

func TestRequiredCheckInputsAreVerifiedBeforeLaunchingCommands(t *testing.T) {
	dir := t.TempDir()
	check := domain.Check{Name: "compile", Command: "nonexistent-command", RequiredPaths: []string{"src/entry"}, TimeoutSeconds: 1}
	results, quiet, err := Run(context.Background(), dir, []domain.Check{check}, nil)
	var failure *Failure
	if !quiet || !errors.As(err, &failure) || failure.Kind != "check_inputs" || len(results) != 1 || results[0].Passed {
		t.Fatal("missing input launched a command or passed", results, quiet, err)
	}
	if err := os.Mkdir(filepath.Join(dir, "src"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src/entry"), []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	_, _, err = Run(context.Background(), dir, []domain.Check{check}, nil)
	if !errors.As(err, &failure) || failure.Kind != "check_start" {
		t.Fatal("launch/configuration failure became product failure", err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	check.Command, check.Args = binary, []string{"-test.run=^TestCheckSubprocess$", "--", "fail"}
	_, _, err = Run(context.Background(), dir, []domain.Check{check}, os.Environ())
	if !errors.As(err, &failure) || failure.Kind != "check_failed" {
		t.Fatal("real command failure lost classification", err)
	}
}

func TestRequiredInputsCannotEscapeThroughSymlinks(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "entry"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "linked")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	check := domain.Check{Name: "compile", Command: "unused", RequiredPaths: []string{"linked/entry"}, TimeoutSeconds: 1}
	_, quiet, err := Run(context.Background(), dir, []domain.Check{check}, nil)
	var failure *Failure
	if !quiet || !errors.As(err, &failure) || failure.Kind != "check_inputs" {
		t.Fatal("symlink input escaped fixed workspace", err)
	}
}
