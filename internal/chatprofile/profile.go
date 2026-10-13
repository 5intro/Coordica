// Package chatprofile owns a private, limited-client startup configuration.
// Tool arguments can never select or replace it.
package chatprofile

import (
	"bytes"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"coordica/internal/filelock"
	"coordica/internal/jsoninput"
	"coordica/internal/localfile"
)

type Profile struct {
	CenterURL    string `json:"center_url"`
	CredentialID string `json:"credential_id"`
	Token        string `json:"token"`
	TLSCA        string `json:"tls_ca,omitempty"`
}

func ValidatePath(path string) error {
	if !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\r\n") {
		return errors.New("absolute private chat profile required")
	}
	// Reject symlinks in every existing component, not just the final file.
	current := filepath.VolumeName(path) + string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(path, current), string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, e := os.Lstat(current)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return errors.New("cannot inspect chat profile path")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("chat profile path cannot contain symlinks")
		}
	}
	return nil
}
func Validate(p Profile) error {
	u, e := url.Parse(p.CenterURL)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return errors.New("invalid chat center URL")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || ip != nil && ip.IsLoopback())) {
		return errors.New("chat requires loopback HTTP or HTTPS")
	}
	if !regexp.MustCompile(`^chat_[a-f0-9]{32}$`).MatchString(p.Token) || !regexp.MustCompile(`^client_[a-f0-9]{32}$`).MatchString(p.CredentialID) {
		return errors.New("limited chat credential required")
	}
	return nil
}
func Load(path string) (Profile, error) {
	var p Profile
	if e := ValidatePath(path); e != nil {
		return p, e
	}
	f, e := os.Open(path)
	if e != nil {
		return p, errors.New("cannot open private chat profile")
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || localfile.CheckPrivate(path) != nil {
		return p, errors.New("chat profile must be a private regular file")
	}
	raw, e := io.ReadAll(io.LimitReader(f, (16<<10)+1))
	if e != nil {
		return p, errors.New("cannot read private chat profile")
	}
	if e = jsoninput.Decode(raw, &p, 16<<10); e != nil {
		return p, errors.New("invalid private chat profile")
	}
	return p, Validate(p)
}
func Write(path string, raw []byte) error {
	if e := ValidatePath(path); e != nil {
		return e
	}
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return errors.New("cannot create profile directory")
	}
	lock := path + ".write.lock"
	if e := ValidatePath(lock); e != nil {
		return e
	}
	release, e := filelock.Acquire(lock)
	if e != nil {
		return errors.New("profile writer already active")
	}
	defer release()
	if e = localfile.Protect(lock); e != nil {
		return e
	}
	if old, e := os.ReadFile(path); e == nil {
		if bytes.Equal(old, raw) {
			return nil
		}
		return errors.New("different profile exists; choose a new profile path")
	} else if !os.IsNotExist(e) {
		return errors.New("cannot inspect existing profile")
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".chat-profile-pending-")
	if e != nil {
		return errors.New("cannot stage private profile")
	}
	defer os.Remove(f.Name())
	if e = localfile.Protect(f.Name()); e == nil {
		_, e = f.Write(raw)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil || closeErr != nil {
		return errors.New("cannot stage private profile")
	}
	// Hard-link publication is atomic and refuses a destination created by any
	// other writer, including writers that do not honor the local lock.
	if e = os.Link(f.Name(), path); e != nil {
		return errors.New("profile publication refused; destination exists or filesystem cannot publish safely")
	}
	return nil
}
