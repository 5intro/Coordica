//go:build windows

package localfile

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")

// Protect sets the current process user as owner with a protected user-only
// DACL. Directory ACEs inherit that specific user SID, independent of an
// elevated token's default Administrators owner. Protect children explicitly
// before writing to set their ownership as well; ownership does not inherit.
func Protect(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Errorf("private file user: %w", err)
	}
	sid := user.User.Sid.String()
	sddl := "D:P(A;;FA;;;" + sid + ")"
	if info.IsDir() {
		sddl = "D:P(A;OICI;FA;;;" + sid + ")"
	}
	descriptor, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return fmt.Errorf("private security descriptor: %w", err)
	}
	// Set owner and DACL together while the existing access still permits
	// WRITE_OWNER and WRITE_DAC. Do not enable privileges or accept another owner.
	const security = windows.OWNER_SECURITY_INFORMATION | windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, security, user.User.Sid, nil, dacl, nil); err != nil {
		return fmt.Errorf("private file owner and ACL: %w", err)
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
