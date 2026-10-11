package agent

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// pi resumes the cwd recorded in its session header. Selecting a session
// file does not override that cwd, so reject a moved/recreated worktree.
func validatePiResume(req Request) error {
	if !filepath.IsAbs(req.ResumeSession) {
		return errors.New("pi resume requires the exact absolute session-file handle")
	}
	f, err := os.Open(req.ResumeSession)
	if err != nil {
		return errors.New("pi resume session file is unavailable")
	}
	defer f.Close()
	line, err := bufio.NewReader(io.LimitReader(f, 64<<10+1)).ReadString('\n')
	if err != nil && err != io.EOF {
		return errors.New("pi resume session header could not be read")
	}
	if len(line) > 64<<10 {
		return errors.New("pi resume session header exceeds size limit")
	}
	var header struct {
		Type string `json:"type"`
		Cwd  string `json:"cwd"`
	}
	if json.Unmarshal([]byte(line), &header) != nil || header.Type != "session" || !filepath.IsAbs(header.Cwd) {
		return errors.New("pi resume session header is invalid")
	}
	cwd, err := filepath.EvalSymlinks(header.Cwd)
	if err != nil {
		return errors.New("pi resume original working directory is unavailable")
	}
	work, err := filepath.EvalSymlinks(req.WorkDir)
	if err != nil {
		return errors.New("pi resume requested working directory is unavailable")
	}
	if runtime.GOOS == "windows" {
		cwd = strings.ToLower(cwd)
		work = strings.ToLower(work)
	}
	if cwd != work {
		return errors.New("pi resume requires the same canonical working directory; start a new session for a recreated worktree")
	}
	return nil
}
