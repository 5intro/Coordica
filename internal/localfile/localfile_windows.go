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

// CheckPrivate validates the same user-only protected DACL as Protect. POSIX
// mode bits on Windows do not describe who can read a credential file.
func CheckPrivate(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return os.ErrPermission
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil || !owner.Equals(user.User.Sid) {
		return os.ErrPermission
	}
	control, _, err := sd.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		return os.ErrPermission
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil || dacl.AceCount != 1 {
		return os.ErrPermission
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err = windows.GetAce(dacl, 0, &ace); err != nil {
		return err
	}
	sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
	const allAccess = windows.STANDARD_RIGHTS_REQUIRED | windows.SYNCHRONIZE | 0x1ff
	if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || !sid.Equals(user.User.Sid) || ace.Mask != allAccess || ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
		return os.ErrPermission
	}
	return nil
}
