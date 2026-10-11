//go:build windows

package localfile

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")
var advapi32 = syscall.NewLazyDLL("advapi32.dll")

// Protect uses an owner-only protected DACL. Directories also carry inheritable
// permissions for new child files and directories; existing child objects
// should be protected explicitly when migrating older data.
func Protect(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	sddl := "D:P(A;;FA;;;OW)"
	if info.IsDir() {
		sddl = "D:P(A;OICI;FA;;;OW)"
	}
	text, err := syscall.UTF16PtrFromString(sddl)
	if err != nil {
		return err
	}
	var descriptor uintptr
	ok, _, err := advapi32.NewProc("ConvertStringSecurityDescriptorToSecurityDescriptorW").Call(uintptr(unsafe.Pointer(text)), 1, uintptr(unsafe.Pointer(&descriptor)), 0)
	if ok == 0 {
		return fmt.Errorf("private security descriptor: %w", err)
	}
	defer kernel32.NewProc("LocalFree").Call(descriptor)
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	ok, _, err = advapi32.NewProc("SetFileSecurityW").Call(uintptr(unsafe.Pointer(name)), 0x80000004, descriptor)
	if ok == 0 {
		return fmt.Errorf("private file ACL: %w", err)
	}
	return nil
}

// Replace replaces an existing destination without an unlink window and asks
// Windows to flush the move before returning.
func Replace(source, dest string) error {
	from, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := syscall.UTF16PtrFromString(dest)
	if err != nil {
		return err
	}
	ok, _, err := kernel32.NewProc("MoveFileExW").Call(uintptr(unsafe.Pointer(from)), uintptr(unsafe.Pointer(to)), 0x1|0x8)
	if ok == 0 {
		return fmt.Errorf("replace persisted file: %w", err)
	}
	return nil
}
