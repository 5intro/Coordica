package chatprofile

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"coordica/internal/filelock"
	"coordica/internal/jsoninput"
	"coordica/internal/localfile"
)

// Hold the process lock from preflight to publication/cleanup. OS locks are
// released on crashes; the private pending envelope survives for grant replay.
func LockInit(path string) (func(), error) {
	if e := ValidatePath(path); e != nil {
		return nil, e
	}
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return nil, e
	}
	lock := path + ".init.lock"
	if e := ValidatePath(lock); e != nil {
		return nil, e
	}
	release, e := filelock.Acquire(lock)
	if e != nil {
		return nil, errors.New("shared profile initialization already active")
	}
	if e = localfile.Protect(lock); e != nil {
		release()
		return nil, e
	}
	return release, nil
}

type Pending struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	CenterURL string `json:"center_url"`
	TLSCA     string `json:"tls_ca,omitempty"`
	ChatScope string `json:"chat_scope"`
	NodeID    string `json:"node_id,omitempty"`
}

func LoadPending(path string) (Pending, error) {
	var p Pending
	path += ".init.pending"
	if e := ValidatePath(path); e != nil {
		return p, e
	}
	if e := localfile.CheckPrivate(path); e != nil {
		return p, e
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		return p, e
	}
	if e = jsoninput.Decode(raw, &p, 16<<10); e != nil {
		return p, errors.New("invalid private initialization envelope")
	}
	if p.Key == "" {
		return p, errors.New("missing pending grant identity")
	}
	return p, nil
}
func SavePending(path string, p Pending) error {
	dest := path + ".init.pending"
	if e := ValidatePath(dest); e != nil {
		return e
	}
	raw, e := json.Marshal(p)
	if e != nil {
		return e
	}
	return localfile.WritePrivate(dest, raw)
}
func ClearPending(path string) error {
	dest := path + ".init.pending"
	if e := ValidatePath(dest); e != nil {
		return e
	}
	e := os.Remove(dest)
	if os.IsNotExist(e) {
		return nil
	}
	return e
}
