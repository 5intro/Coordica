//go:build windows

package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func assertBackupPrivate(t *testing.T, path string, directory bool) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil || info.IsDir() != directory {
		t.Fatalf("backup object type for %s: %v", path, err)
	}
	assertBackupOwnerOnlyDACL(t, path, directory, true)
}

func assertBackupOwnerOnlyDACL(t *testing.T, path string, directory, protected bool) {
	t.Helper()
	// FILE_ALL_ACCESS combines the standard, synchronization and file-specific
	// rights; x/sys/windows does not expose that aggregate constant.
	const fileAllAccess = windows.STANDARD_RIGHTS_REQUIRED | windows.SYNCHRONIZE | 0x1ff
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	rights, err := windows.CreateWellKnownSid(windows.WinCreatorOwnerRightsSid)
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil || !owner.Equals(user.User.Sid) {
		t.Fatalf("backup object is not owned by current user: %s (%v)", path, err)
	}
	control, _, err := sd.Control()
	if err != nil || (protected && control&windows.SE_DACL_PROTECTED == 0) {
		t.Fatalf("backup DACL is not protected: %s (%v)", path, err)
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil || dacl.AceCount == 0 {
		t.Fatalf("backup DACL missing owner access: %s (%v)", path, err)
	}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			t.Fatal(err)
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			t.Fatalf("unexpected backup ACE type: %s", path)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !sid.Equals(user.User.Sid) && !sid.Equals(rights) {
			t.Fatalf("backup ACL grants another principal %s access: %s", sid.String(), path)
		}
		if ace.Mask&fileAllAccess != fileAllAccess || ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
			t.Fatalf("backup ACL does not grant owner full access: %s", path)
		}
		if directory && ace.Header.AceFlags&(windows.OBJECT_INHERIT_ACE|windows.CONTAINER_INHERIT_ACE) != windows.OBJECT_INHERIT_ACE|windows.CONTAINER_INHERIT_ACE {
			t.Fatalf("backup directory ACL does not protect new children: %s", path)
		}
	}
}

func TestBackupPlanningWindowsRootsProtectNewChildren(t *testing.T) {
	_, _, backup := planningReceiptBackup(t)
	dest := filepath.Join(t.TempDir(), "restored")
	if err := RestoreBackup(context.Background(), backup, dest); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{filepath.Dir(backup), backup, dest} {
		assertBackupPrivate(t, root, true)
		// No explicit Protect call: children must already inherit privacy before
		// any caller writes their content, including SQLite and tool adapters.
		child := filepath.Join(root, "new-private-child")
		if err := os.Mkdir(child, 0700); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(child, "receipt.json")
		if err := os.WriteFile(file, []byte("confidential test receipt"), 0600); err != nil {
			t.Fatal(err)
		}
		assertBackupOwnerOnlyDACL(t, child, true, false)
		assertBackupOwnerOnlyDACL(t, file, false, false)
	}
}
