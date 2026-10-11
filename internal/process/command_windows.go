//go:build windows

package process

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var getSystemDirectory = kernel32.NewProc("GetSystemDirectoryW")

func commandFor(spec Spec) (*exec.Cmd, error) {
	name := spec.Executable
	if strings.ContainsAny(name, `\/`) && !filepath.IsAbs(name) {
		var err error
		name, err = filepath.Abs(filepath.Join(spec.Dir, name))
		if err != nil {
			return nil, err
		}
	}
	resolved, err := exec.LookPath(name)
	if err != nil {
		// In particular, preserve exec.ErrDot; no current-directory fallback.
		return nil, err
	}
	if !isBatchShim(resolved) {
		return exec.Command(resolved, spec.Args...), nil
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: shim is not a regular file", ErrUnsafeBatchCommand)
	}
	interpreter, err := systemCommandInterpreter()
	if err != nil {
		return nil, err
	}
	line, err := buildBatchCommand(interpreter, resolved, spec.Args)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(interpreter)
	// cmd.exe has its own parsing rules. Go's generic Windows argv escaping
	// cannot represent the double outer quotes required by /s /c correctly.
	cmd.Args = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: line}
	return cmd, cmd.Err
}

func systemCommandInterpreter() (string, error) {
	// Query the OS rather than trusting COMSPEC, PATH, or a child-supplied
	// SystemRoot. This is the system directory containing cmd.exe.
	buffer := make([]uint16, 32768)
	n, _, err := getSystemDirectory.Call(uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if n == 0 {
		return "", winError("GetSystemDirectoryW", err)
	}
	if n >= uintptr(len(buffer)) {
		return "", fmt.Errorf("system directory exceeds supported path length")
	}
	return filepath.Join(syscall.UTF16ToString(buffer[:n]), "cmd.exe"), nil
}
