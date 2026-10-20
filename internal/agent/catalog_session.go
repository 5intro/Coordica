package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"coordica/internal/filelock"
	"coordica/internal/jsoninput"
	"coordica/internal/localfile"
)

// Only metadata probes use this private state. Never load or adopt sessions
// found in a harness's history, and never use a probe session for a task.
type catalogSession struct {
	Version   int    `json:"version"`
	Identity  string `json:"identity"`
	SessionID string `json:"session_id"`
	Pending   bool   `json:"pending"`
	path      string
	workDir   string
}

func lockCatalogSession(def Definition, executable string) (*catalogSession, func(), error) {
	path, err := filepath.Abs(executable)
	if err != nil {
		return nil, nil, err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return nil, nil, err
	}
	identity, err := json.Marshal([]any{def.ID, path, def.StartArgs, def.AuthMethod, def.ResumeMethod, def.CloseMethod})
	if err != nil {
		return nil, nil, err
	}
	digest := sha256.Sum256(identity)
	id := hex.EncodeToString(digest[:])
	root := os.Getenv("COORDICA_CATALOG_STATE_DIR")
	if root == "" {
		root, err = os.UserConfigDir()
		if err != nil {
			return nil, nil, err
		}
		root = filepath.Join(root, "Coordica", "catalog-sessions")
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, nil, err
	}
	dir := filepath.Join(root, id)
	for _, name := range []string{root, dir, filepath.Join(dir, "workspace")} {
		if err = os.MkdirAll(name, 0700); err != nil {
			return nil, nil, err
		}
		info, statErr := os.Lstat(name)
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, nil, errors.New("catalog state requires ordinary private directories")
		}
		if err = localfile.Protect(name); err != nil {
			return nil, nil, err
		}
	}
	lockPath := filepath.Join(dir, "session.lock")
	if info, err := os.Lstat(lockPath); err == nil && !info.Mode().IsRegular() || err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, errors.New("catalog lock is not a regular file")
	}
	unlock, err := filelock.Acquire(lockPath)
	if err != nil {
		return nil, nil, err
	}
	session := &catalogSession{Version: 1, Identity: id, path: filepath.Join(dir, "session.json"), workDir: filepath.Join(dir, "workspace")}
	if info, err := os.Lstat(session.path); errors.Is(err, os.ErrNotExist) {
		return session, unlock, nil
	} else if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		unlock()
		return nil, nil, errors.New("invalid catalog session state")
	}
	if err = localfile.CheckPrivate(session.path); err == nil {
		var data []byte
		data, err = os.ReadFile(session.path)
		if err == nil {
			err = jsoninput.Decode(data, session, 4096)
		}
	}
	if err != nil || session.Version != 1 || session.Identity != id || len(session.SessionID) > 512 || session.Pending || session.SessionID == "" {
		unlock()
		return nil, nil, errors.New("catalog session needs recovery; refusing another persistent session")
	}
	return session, unlock, nil
}

func (s *catalogSession) save() error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return localfile.WritePrivate(s.path, data)
}
