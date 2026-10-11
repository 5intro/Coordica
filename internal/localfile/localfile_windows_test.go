//go:build windows

package localfile

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestProtectWindowsSetsUserOwnerAndInheritsUserOnlyAccessBeforeWrites(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private root")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	// Elevated CI reproduces the group-owner case explicitly. Normal tokens
	// still exercise Protect and check real ownership instead of assuming it.
	if windows.GetCurrentProcessToken().IsElevated() {
		admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
		if err != nil {
			t.Fatal(err)
		}
		if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION, admins, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := Protect(dir); err != nil {
		t.Fatal(err)
	}
	assertWindowsPrivateSecurity(t, dir, true, true)
	child := filepath.Join(dir, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(child, "receipt.json")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	// Before any content, verify inheritance grants only the specific user;
	// the token's default owner is independent and may still be Administrators.
	assertWindowsPrivateSecurity(t, child, true, false)
	assertWindowsPrivateSecurity(t, path, false, false)
	for _, object := range []string{child, path} {
		if err := Protect(object); err != nil {
			t.Fatal(err)
		}
	}
	assertWindowsPrivateSecurity(t, child, true, true)
	assertWindowsPrivateSecurity(t, path, false, true)
	for _, content := range []string{"first confidential receipt", "replacement receipt"} {
		if err := WritePrivate(path, []byte(content)); err != nil {
			t.Fatal(err)
		}
		assertWindowsPrivateSecurity(t, path, false, true)
		raw, err := os.ReadFile(path)
		if err != nil || string(raw) != content {
			t.Fatal("private replacement content", err)
		}
	}
}

func assertWindowsPrivateSecurity(t *testing.T, path string, directory, explicitlyProtected bool) {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil || sd == nil {
		t.Fatal("read private security descriptor", err)
	}
	if explicitlyProtected {
		owner, _, err := sd.Owner()
		if err != nil || owner == nil || !owner.Equals(user.User.Sid) {
			t.Fatalf("private object owner is not current user: %s (%v)", path, err)
		}
	}
	control, _, err := sd.Control()
	if err != nil || (control&windows.SE_DACL_PROTECTED != 0) != explicitlyProtected {
		t.Fatalf("private DACL protection mismatch: %s (%v)", path, err)
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil || dacl.AceCount != 1 {
		t.Fatalf("expected exactly one private user ACE: %s (%v)", path, err)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil {
		t.Fatal(err)
	}
	sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
	const fileAllAccess = windows.STANDARD_RIGHTS_REQUIRED | windows.SYNCHRONIZE | 0x1ff
	if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || !sid.Equals(user.User.Sid) || ace.Mask != fileAllAccess || ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
		t.Fatalf("private ACE is not current-user full access: %s", path)
	}
	if directory && ace.Header.AceFlags&(windows.OBJECT_INHERIT_ACE|windows.CONTAINER_INHERIT_ACE) != windows.OBJECT_INHERIT_ACE|windows.CONTAINER_INHERIT_ACE {
		t.Fatalf("private directory ACE is not inheritable: %s", path)
	}
	if !explicitlyProtected && ace.Header.AceFlags&windows.INHERITED_ACE == 0 {
		t.Fatalf("new empty object's private ACE did not inherit: %s", path)
	}
}
