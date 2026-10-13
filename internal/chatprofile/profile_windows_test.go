//go:build windows

package chatprofile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"coordica/internal/localfile"
	"golang.org/x/sys/windows"
)

// Run on Windows CI: Go's apparent 0666/0444 permissions do not express ACL
// privacy. The profile must load with our owner-only DACL and reject wider ACLs.
func TestWindowsProfileValidatesOwnerAndProtectedDACL(t *testing.T) {
	dir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "host.json")
	p := Profile{CenterURL: "https://example.com", CredentialID: "client_" + strings.Repeat("a", 32), Token: "chat_" + strings.Repeat("b", 32)}
	raw, _ := json.Marshal(p)
	if e = localfile.WritePrivate(path, raw); e != nil {
		t.Fatal(e)
	}
	if _, e = Load(path); e != nil {
		t.Fatal("protected profile rejected using Unix bits", e)
	}
	user, e := windows.GetCurrentProcessToken().GetTokenUser()
	if e != nil {
		t.Fatal(e)
	}
	sd, e := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.User.Sid.String() + ")(A;;GR;;;WD)")
	if e != nil {
		t.Fatal(e)
	}
	dacl, _, e := sd.DACL()
	if e != nil {
		t.Fatal(e)
	}
	if e = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); e != nil {
		t.Fatal(e)
	}
	if _, e = Load(path); e == nil {
		t.Fatal("public ACL profile accepted")
	}
	if e = localfile.Protect(path); e != nil {
		t.Fatal(e)
	}
	if e = os.Remove(path); e != nil {
		t.Fatal(e)
	}
	if e = os.Mkdir(path, 0700); e != nil {
		t.Fatal(e)
	}
	if _, e = Load(path); e == nil {
		t.Fatal("directory profile accepted")
	}
}
