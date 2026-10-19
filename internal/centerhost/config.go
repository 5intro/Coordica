// Package centerhost owns trusted, fixed local center setup. It does not accept
// MCP-selected commands, paths, roles, URLs, or process identifiers.
package centerhost

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"coordica/internal/domain"
	"coordica/internal/filelock"
	"coordica/internal/jsoninput"
	"coordica/internal/localfile"
)

type Config struct {
	Version          int            `json:"version"`
	Role             string         `json:"role"`
	Executable       string         `json:"executable"`
	DataDir          string         `json:"data_dir"`
	ChatProfile      string         `json:"chat_profile"`
	Repository       string         `json:"repository"`
	TargetRepository string         `json:"target_repository,omitempty"`
	Branch           string         `json:"branch"`
	Name             string         `json:"name"`
	ConsoleListen    string         `json:"console_listen"`
	RunnerListen     string         `json:"runner_listen"`
	RunnerURL        string         `json:"runner_url"`
	CodexExecutable  string         `json:"codex_executable"`
	RuntimePath      string         `json:"runtime_path"`
	HTTPSProxy       string         `json:"https_proxy,omitempty"`
	GitAuthFile      string         `json:"git_auth_file,omitempty"`
	GitAuthRemote    string         `json:"git_auth_remote,omitempty"`
	GitHubRepository string         `json:"github_repository,omitempty"`
	GitHubTokenFile  string         `json:"github_token_file,omitempty"`
	Checks           []domain.Check `json:"checks"`
}

func (c Config) RepositoryURL() string {
	if c.TargetRepository != "" {
		return c.TargetRepository
	}
	return c.Repository
}
func (c Config) ConsoleURL() string { return "http://" + c.ConsoleListen }
func (c Config) TLS() bool          { return c.RunnerListen != c.ConsoleListen }
func Validate(c Config, path string) error {
	if c.GitHubRepository != "" || c.GitHubTokenFile != "" {
		repository, e := domain.GitHubRepository(c.RepositoryURL())
		if e != nil || !strings.EqualFold(repository, c.GitHubRepository) || !filepath.IsAbs(c.GitHubTokenFile) || strings.ContainsAny(c.GitHubTokenFile, "\x00\r\n") {
			return errors.New("GitHub adapter needs a fixed matching repository and absolute center token file")
		}
	}
	if c.GitAuthFile != "" || c.GitAuthRemote != "" {
		if !filepath.IsAbs(c.GitAuthFile) || c.GitAuthRemote != c.RepositoryURL() || !strings.HasPrefix(c.GitAuthRemote, "https://") || strings.ContainsAny(c.GitAuthFile, "\x00\r\n") {
			return errors.New("Git authorization must bind the fixed HTTPS repository and absolute secret file")
		}
	}
	if c.Version != 1 || c.Role != "center" {
		return errors.New("trusted center host configuration required")
	}
	for _, value := range []string{path, c.Executable, c.DataDir, c.ChatProfile, c.Repository, c.CodexExecutable} {
		if !filepath.IsAbs(value) || strings.ContainsAny(value, "\x00\r\n") {
			return errors.New("host paths must be fixed absolute paths without control characters")
		}
	}
	for _, value := range []string{c.RuntimePath, c.Name, c.Branch} {
		if strings.ContainsAny(value, "\x00\r\n") {
			return errors.New("invalid host configuration")
		}
	}
	if len(c.Checks) > 0 {
		if e := domain.ValidateProject(domain.Project{Name: c.Name, RepositoryURL: c.RepositoryURL(), SourceRepository: c.Repository, TargetBranch: c.Branch, Checks: c.Checks}); e != nil {
			return e
		}
	}
	if strings.TrimSpace(c.Name) == "" || c.Branch == "" || strings.HasPrefix(c.Branch, "-") || strings.Contains(c.Branch, "..") {
		return errors.New("name and literal repository branch required")
	}
	for _, endpoint := range []string{c.ConsoleListen, c.RunnerListen} {
		host, port, e := net.SplitHostPort(endpoint)
		n, ne := strconv.Atoi(port)
		if e != nil || ne != nil || n < 1 || n > 65535 || net.ParseIP(host) == nil {
			return errors.New("fixed IP listener and port 1..65535 required")
		}
		if endpoint == c.ConsoleListen && !net.ParseIP(host).IsLoopback() {
			return errors.New("Owner console must be loopback-only")
		}
		if !c.TLS() && !net.ParseIP(host).IsLoopback() {
			return errors.New("remote Runner listener requires separate TLS")
		}
	}
	if c.TLS() {
		u, e := url.Parse(c.RunnerURL)
		if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("separate Runner listener requires a fixed plain HTTPS runner URL")
		}
	}
	if c.HTTPSProxy != "" {
		u, e := url.Parse(c.HTTPSProxy)
		if e != nil || u.Hostname() == "" || u.Scheme != "http" && u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(c.HTTPSProxy, "\x00\r\n") {
			return errors.New("only a non-secret fixed HTTP(S) proxy origin may be saved")
		}
	}
	// Resolve parent symlinks before comparing assets. A configuration/profile
	// cannot overwrite data credentials, repository source, or each other.
	assets := []string{path, c.ChatProfile, c.Executable, c.CodexExecutable}
	for _, secret := range []string{c.GitAuthFile, c.GitHubTokenFile} {
		if secret != "" {
			assets = append(assets, secret)
		}
	}
	for i, a := range assets {
		ra, e := resolved(a)
		if e != nil {
			return e
		}
		for _, b := range assets[i+1:] {
			rb, e := resolved(b)
			if e != nil {
				return e
			}
			ai, ae := os.Stat(a)
			bi, be := os.Stat(b)
			if samePath(ra, rb) || ae == nil && be == nil && os.SameFile(ai, bi) {
				return errors.New("host role assets must use distinct fixed paths")
			}
		}
	}
	for _, a := range []string{path, c.ChatProfile} {
		for _, tree := range []string{c.DataDir} {
			ra, e := resolved(a)
			if e != nil {
				return e
			}
			rb, e := resolved(tree)
			if e != nil {
				return e
			}
			rel, e := filepath.Rel(rb, ra)
			if e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return errors.New("host config and limited profile must live outside data and repository directories")
			}
		}
	}
	ra, e := resolved(c.DataDir)
	if e != nil {
		return e
	}
	rb, e := resolved(c.Repository)
	if e != nil {
		return e
	}
	for _, pair := range [][2]string{{ra, rb}} {
		rel, e := filepath.Rel(pair[0], pair[1])
		if e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return errors.New("center data and repository must be separate")
		}
	}
	for _, secret := range []string{c.GitAuthFile, c.GitHubTokenFile} {
		if secret == "" {
			continue
		}
		rs, e := resolved(secret)
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(rb, rs)
		if e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return errors.New("Git credentials must live outside the source repository")
		}
	}
	return ValidateCredentialPaths(c.DataDir, []string{path, c.ChatProfile, c.GitAuthFile, c.GitHubTokenFile})
}

// ValidateCredentialPaths rejects aliases of center assets before bootstrap can
// create them. Direct serve and saved host configuration share this boundary.
// Extra protected paths cover explicitly selected TLS files outside DataDir.
func ValidateCredentialPaths(data string, assets []string, protected ...string) error {
	for _, name := range []string{"owner.token", "coordica.db", "coordica.db-wal", "coordica.db-shm", "center.lock", "runner.json", "center-identity.json", "center-operation.json", "center.key", "center.crt"} {
		protected = append(protected, filepath.Join(data, name))
	}
	for _, asset := range assets {
		if asset == "" {
			continue
		}
		absolute, e := filepath.Abs(asset)
		if e != nil {
			return e
		}
		ra, e := resolved(absolute)
		if e != nil {
			return e
		}
		ai, ae := os.Stat(absolute)
		for _, other := range protected {
			if other == "" {
				continue
			}
			other, e = filepath.Abs(other)
			if e != nil {
				return e
			}
			rb, e := resolved(other)
			if e != nil {
				return e
			}
			bi, be := os.Stat(other)
			if samePath(ra, rb) || ae == nil && be == nil && os.SameFile(ai, bi) {
				return errors.New("credential or role file aliases a protected center asset")
			}
		}
	}
	return nil
}
func samePath(a, b string) bool { return strings.EqualFold(a, b) }
func resolved(path string) (string, error) {
	result, e := filepath.EvalSymlinks(path)
	if os.IsNotExist(e) {
		parent := filepath.Dir(path)
		if parent == path {
			return "", e
		}
		base, e := resolved(parent)
		return filepath.Join(base, filepath.Base(path)), e
	}
	return result, e
}
func Load(path string) (Config, error) {
	var c Config
	raw, e := os.ReadFile(path)
	if e != nil {
		return c, e
	}
	if e = jsoninput.Decode(raw, &c, 64<<10); e != nil {
		return c, e
	}
	return c, Validate(c, path)
}
func Save(path string, c Config) error {
	if c.Checks == nil {
		c.Checks = []domain.Check{}
	}
	for i := range c.Checks {
		if c.Checks[i].Args == nil {
			c.Checks[i].Args = []string{}
		}
	}
	if e := Validate(c, path); e != nil {
		return e
	}
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	release, e := filelock.Acquire(path + ".lock")
	if e != nil {
		return errors.New("host configuration is busy")
	}
	defer release()
	old, e := Load(path)
	if e == nil {
		if !reflect.DeepEqual(old, c) {
			return errors.New("saved host configuration conflicts; choose a new host/data/profile instance")
		}
		return localfile.Protect(path)
	}
	if !os.IsNotExist(e) {
		return errors.New("existing host configuration invalid; preserved")
	}
	raw, e := json.MarshalIndent(c, "", "  ")
	if e != nil {
		return e
	}
	return localfile.WritePrivate(path, append(bytes.TrimSpace(raw), '\n'))
}
