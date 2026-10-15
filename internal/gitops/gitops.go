// Package gitops handles fixed Git objects and controlled Git transports. It has no
// knowledge of task authorization, leases, databases, or HTTP. Its callers must
// persist intents and decide whether protected changes have been approved.
package gitops

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"coordica/internal/process"
)

var (
	ErrInvalidOID              = errors.New("gitops: invalid full object ID")
	ErrInvalidRef              = errors.New("gitops: invalid reference")
	ErrGit                     = errors.New("gitops: Git command failed")
	ErrUnsafeConfig            = errors.New("gitops: executable or included repository config")
	ErrInvalidBundle           = errors.New("gitops: invalid bundle")
	ErrNotAncestor             = errors.New("gitops: expected ancestor relationship absent")
	ErrOutOfScope              = errors.New("gitops: changes outside scope")
	ErrUnsupportedObject       = errors.New("gitops: unsupported object or path")
	ErrDirty                   = errors.New("gitops: worktree differs from fixed commit")
	ErrConflict                = errors.New("gitops: merge conflict")
	ErrNoChanges               = errors.New("gitops: head already contained in main")
	ErrCAS                     = errors.New("gitops: remote reference changed")
	ErrUnknown                 = errors.New("gitops: publication result requires reconciliation")
	ErrOutputLimit             = errors.New("gitops: command output limit exceeded")
	ErrWorkingDirectoryTooLong = errors.New("gitops: Windows working directory is too long")
)

// Error preserves a machine-readable kind without requiring stderr parsing.
type Error struct {
	Op       string
	Kind     error
	Detail   string
	ExitCode int
}

func (e *Error) Error() string {
	if e.Detail == "" {
		return e.Op + ": " + e.Kind.Error()
	}
	return e.Op + ": " + e.Kind.Error() + ": " + e.Detail
}
func (e *Error) Unwrap() error { return e.Kind }
func fault(op string, kind error, detail string) error {
	return &Error{Op: op, Kind: kind, Detail: detail}
}

type OID string

func (o OID) String() string { return string(o) }

// ParseOID accepts complete SHA-1 or SHA-256 IDs, never revisions or prefixes.
func ParseOID(s string) (OID, error) {
	if len(s) != 40 && len(s) != 64 {
		return "", ErrInvalidOID
	}
	if _, err := hex.DecodeString(s); err != nil || strings.Trim(s, "0") == "" {
		return "", ErrInvalidOID
	}
	return OID(strings.ToLower(s)), nil
}
func validIDs(ids ...OID) error {
	for _, o := range ids {
		n, err := ParseOID(string(o))
		if err != nil || n != o {
			return ErrInvalidOID
		}
	}
	return nil
}

type Identity struct {
	Name  string
	Email string
}

func identityEnv(id Identity) ([]string, error) {
	if id.Name == "" {
		id.Name = "Coordica"
	}
	if id.Email == "" {
		id.Email = "coordica@localhost"
	}
	if strings.ContainsAny(id.Name+id.Email, "\x00\r\n<>") {
		return nil, fmt.Errorf("invalid Git identity")
	}
	return []string{"GIT_AUTHOR_NAME=" + id.Name, "GIT_AUTHOR_EMAIL=" + id.Email, "GIT_COMMITTER_NAME=" + id.Name, "GIT_COMMITTER_EMAIL=" + id.Email}, nil
}

type Client struct {
	GitPath              string
	analysisPrivatePaths []string
	authRemote           string
	authorization        string
}

func New(gitPath string) *Client {
	if gitPath == "" {
		gitPath = "git"
	}
	return &Client{GitPath: gitPath}
}

// WithHTTPSAuthorization returns a center-only client whose Authorization
// header is bound to exactly one HTTPS remote. A Runner uses the plain client.
// The header is supplied through child environment, never command arguments.
func (g *Client) WithHTTPSAuthorization(remote, authorization string) (*Client, error) {
	u, err := httpsRemote(remote)
	if err != nil {
		return nil, err
	}
	if authorization == "" || strings.ContainsAny(authorization, "\r\n\x00") {
		return nil, fmt.Errorf("invalid HTTPS Authorization value")
	}
	copy := *g
	copy.authRemote = u
	copy.authorization = authorization
	return &copy, nil
}

func httpsRemote(remote string) (string, error) {
	u, err := url.Parse(remote)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || strings.ContainsAny(remote, "\r\n\x00") {
		return "", fmt.Errorf("remote must be an HTTPS URL without credentials, query or fragment")
	}
	if u.Port() != "" {
		n, e := strconv.Atoi(u.Port())
		if e != nil || n < 1 || n > 65535 {
			return "", fmt.Errorf("invalid remote port")
		}
	}
	return u.String(), nil
}
func (g *Client) authEnv(remote string) []string {
	if remote != g.authRemote || g.authorization == "" {
		return nil
	}
	// Keys are added after the fixed configuration list by run.
	return []string{"COORDICA_GIT_AUTHORIZATION=" + g.authorization}
}

const MaxBundleBytes int64 = 64 << 20
const maxOutput = 16 << 20

func controlledEnv(extra []string) []string {
	var env []string
	for _, e := range os.Environ() {
		key := strings.SplitN(e, "=", 2)[0]
		if strings.HasPrefix(strings.ToUpper(key), "GIT_") || key == "GH_TOKEN" || key == "GITHUB_TOKEN" || key == "SSH_AUTH_SOCK" || key == "SSH_AGENT_PID" {
			continue
		}
		env = append(env, e)
	}
	env = append(env, "LC_ALL=C", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_SYSTEM="+os.DevNull, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0", "GIT_NO_REPLACE_OBJECTS=1", "GIT_LFS_SKIP_SMUDGE=1", "GCM_INTERACTIVE=never", "GIT_PAGER=cat")
	// These settings control managed client commands. A remote's trusted
	// receive hooks/protection remain authoritative and are never bypassed.
	settings := [][2]string{{"core.hooksPath", os.DevNull}, {"core.fsmonitor", "false"}, {"core.attributesFile", os.DevNull}, {"credential.helper", ""}, {"protocol.allow", "never"}, {"protocol.file.allow", "always"}, {"protocol.https.allow", "always"}, {"http.followRedirects", "false"}, {"commit.gpgSign", "false"}, {"tag.gpgSign", "false"}, {"diff.external", ""}, {"core.autocrlf", "false"}, {"core.eol", "lf"}, {"core.quotePath", "false"}, {"merge.renames", "true"}, {"uploadpack.packObjectsHook", ""}}
	if runtime.GOOS == "windows" {
		// Managed workspaces and candidate refs can exceed MAX_PATH. Opt in
		// per invocation because user/system Git configuration stays disabled.
		settings = append(settings, [2]string{"core.longpaths", "true"})
	}
	var normalExtra []string
	for _, e := range extra {
		if strings.HasPrefix(e, "COORDICA_GIT_AUTHORIZATION=") {
			settings = append(settings, [2]string{"http.extraHeader", "Authorization: " + strings.TrimPrefix(e, "COORDICA_GIT_AUTHORIZATION=")})
		} else {
			normalExtra = append(normalExtra, e)
		}
	}
	env = append(env, "GIT_CONFIG_COUNT="+strconv.Itoa(len(settings)))
	for i, kv := range settings {
		env = append(env, fmt.Sprintf("GIT_CONFIG_KEY_%d=%s", i, kv[0]), fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", i, kv[1]))
	}
	return append(env, normalExtra...)
}

func (g *Client) run(ctx context.Context, dir string, extra []string, args ...string) ([]byte, error) {
	if err := validateWorkingDirectory(dir); err != nil {
		return nil, err
	}
	argv := []string{"--no-pager"}
	if dir != "" && runtime.GOOS != "windows" {
		argv = append(argv, "-C", dir)
	}
	argv = append(argv, args...)
	spec := process.Spec{Executable: g.GitPath, Args: argv, Env: controlledEnv(extra), GracePeriod: 750 * time.Millisecond, DrainTimeout: 2 * time.Second}
	if dir != "" && runtime.GOOS == "windows" {
		// Git processes -C before reading core.longpaths. Let Go set the
		// native working directory instead, preserving relative Git paths.
		if !filepath.IsAbs(spec.Executable) {
			absolute, err := exec.LookPath(spec.Executable)
			if err != nil {
				return nil, err
			}
			absolute, err = filepath.Abs(absolute)
			if err != nil {
				return nil, err
			}
			spec.Executable = absolute
		}
		spec.Dir = dir
	}
	stdout, stderr, result, err := process.CollectRaw(ctx, spec, maxOutput, 64<<10)
	if ctx.Err() != nil {
		// Context cancellation must not hide a failed tree-stop observation.
		err = errors.Join(err, ctx.Err())
	}
	if errors.Is(err, process.ErrOutputLimit) {
		return stdout, errors.Join(ErrOutputLimit, err)
	}
	if err != nil {
		e := &Error{Op: args[0], Kind: errors.Join(ErrGit, err), Detail: strings.TrimSpace(string(stderr)), ExitCode: result.ExitCode}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			e.ExitCode = exit.ExitCode()
		} else if e.Detail == "" {
			e.Detail = err.Error()
		}
		return stdout, e
	}
	return stdout, nil
}

// Windows appends a directory separator and terminating NUL to its process
// working directory. Reserve both within MAX_PATH even when Git itself can
// access longer object/reference paths with core.longpaths enabled.
const windowsMaxWorkingDirectoryUnits = 258

// Git for Windows also rejects GIT_DIR longer than PATH_MAX-40 UTF-8 bytes
// before loading repository configuration. Reserve five bytes for "/.git".
const windowsMaxRepositoryPathBytes = 260 - 40 - len("/.git")

func checkWindowsWorkingDirectory(absolute string) error {
	units := len(utf16.Encode([]rune(strings.TrimRight(absolute, `/\`))))
	if units > windowsMaxWorkingDirectoryUnits {
		return fault("preflight", ErrWorkingDirectoryTooLong, fmt.Sprintf("repository directory uses %d UTF-16 units; maximum is %d excluding the trailing separator; choose a shorter center or Runner data directory", units, windowsMaxWorkingDirectoryUnits))
	}
	return nil
}

func validateWorkingDirectory(dir string) error {
	if runtime.GOOS != "windows" || dir == "" {
		return nil
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if err := checkWindowsWorkingDirectory(absolute); err != nil {
		return err
	}
	canonical, err := canonicalRepositoryPath(absolute)
	if err != nil {
		return err
	}
	return checkWindowsRepositoryPath(canonical)
}

func checkWindowsRepositoryPath(canonical string) error {
	if err := checkWindowsWorkingDirectory(canonical); err != nil {
		return err
	}
	bytes := len(strings.TrimRight(canonical, `/\`))
	if bytes > windowsMaxRepositoryPathBytes {
		return fault("preflight", ErrWorkingDirectoryTooLong, fmt.Sprintf("resolved repository directory uses %d UTF-8 bytes; maximum is %d because Git limits GIT_DIR independently of core.longpaths; choose a shorter center or Runner data directory", bytes, windowsMaxRepositoryPathBytes))
	}
	return nil
}

// Resolve the longest existing ancestor, including Windows 8.3 names and
// symlinks, before adding not-yet-created directory components.
func canonicalRepositoryPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	var missing []string
	for {
		canonical, err := filepath.EvalSymlinks(absolute)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				canonical = filepath.Join(canonical, missing[i])
			}
			return canonical, nil
		}
		parent := filepath.Dir(absolute)
		if !os.IsNotExist(err) || parent == absolute {
			return "", err
		}
		missing = append(missing, filepath.Base(absolute))
		absolute = parent
	}
}

// guardRepo reads only config key names. Arbitrary external filters/drivers and
// config includes are refused rather than silently executing user configuration.
func (g *Client) guardRepo(ctx context.Context, repo string) error {
	out, err := g.run(ctx, repo, nil, "config", "--local", "--no-includes", "--name-only", "--null", "--list")
	if err != nil {
		return err
	}
	for _, raw := range bytes.Split(out, []byte{0}) {
		key := strings.ToLower(string(raw))
		if key == "" {
			continue
		}
		unsafe := key == "include.path" || strings.HasPrefix(key, "includeif.") || strings.HasPrefix(key, "filter.") || (strings.HasPrefix(key, "merge.") && strings.HasSuffix(key, ".driver")) || (strings.HasPrefix(key, "diff.") && (strings.HasSuffix(key, ".command") || strings.HasSuffix(key, ".textconv"))) || key == "uploadpack.packobjectshook"
		if unsafe {
			return fault("config", ErrUnsafeConfig, "unsupported repository setting")
		}
	}
	return nil
}

func localDir(p string) (string, error) {
	a, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(a)
	if err != nil {
		return "", err
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("repository is not a directory")
	}
	return a, nil
}

func (g *Client) Init(ctx context.Context, dir string, bare bool) error {
	return g.initRepository(ctx, dir, bare, "")
}

func (g *Client) initRepository(ctx context.Context, dir string, bare bool, format string) error {
	if err := validateWorkingDirectory(dir); err != nil {
		return err
	}
	args := []string{"init", "--template=", "--initial-branch=main"}
	if bare {
		args = append(args, "--bare")
	}
	if format != "" {
		args = append(args, "--object-format="+format)
	}
	args = append(args, "--", dir)
	if _, err := g.run(ctx, "", nil, args...); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		// Local upload-pack clears command-scoped Git configuration. Persist
		// this filesystem option in repositories we create for its children.
		_, err := g.run(ctx, dir, nil, "config", "--local", "core.longpaths", "true")
		return err
	}
	return nil
}

// InitObjectFormat initializes an object store matching a verified full OID.
func (g *Client) InitObjectFormat(ctx context.Context, dir string, bare bool, oid OID) error {
	if err := validIDs(oid); err != nil {
		return err
	}
	format := "sha1"
	if len(oid) == 64 {
		format = "sha256"
	}
	return g.initRepository(ctx, dir, bare, format)
}

// Clone supports a local or HTTPS source and creates an independent, unchecked-out
// repository. It never copies Git config, hooks, hardlinks, or submodules.
func (g *Client) Clone(ctx context.Context, source, dir string) error {
	if err := validateWorkingDirectory(dir); err != nil {
		return err
	}
	var src string
	var err error
	if strings.Contains(source, "://") {
		src, err = httpsRemote(source)
	} else {
		src, err = localDir(source)
		if err == nil {
			err = g.guardRepo(ctx, src)
		}
	}
	if err != nil {
		return err
	}
	if _, err = os.Stat(dir); !os.IsNotExist(err) {
		return fmt.Errorf("clone destination must not exist")
	}
	args := []string{"clone", "--no-local", "--no-checkout", "--no-recurse-submodules", "--template="}
	cloneDir, destination := "", dir
	if runtime.GOOS == "windows" {
		absolute, err := filepath.Abs(dir)
		if err != nil {
			return err
		}
		cloneDir, destination = filepath.Dir(absolute), filepath.Base(absolute)
		if err := os.MkdirAll(cloneDir, 0700); err != nil {
			return err
		}
		// Use a short destination argument and persist longpaths for local
		// transport subprocesses. Git may canonicalize GIT_DIR internally,
		// so the resolved repository root still needs the preflight bound.
		args = append(args, "--config", "core.longpaths=true")
	}
	args = append(args, "--", src, destination)
	_, err = g.run(ctx, cloneDir, g.authEnv(src), args...)
	if err != nil {
		return err
	}
	// Center material/candidate refs are outside Git's default branch refspec.
	_, err = g.run(ctx, dir, g.authEnv(src), "fetch", "--no-tags", "--no-recurse-submodules", "--", src, "refs/coordica/*:refs/coordica/*")
	return err
}

func (g *Client) Head(ctx context.Context, repo string) (OID, error) {
	if err := g.guardRepo(ctx, repo); err != nil {
		return "", err
	}
	out, err := g.run(ctx, repo, nil, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return "", err
	}
	return ParseOID(strings.TrimSpace(string(out)))
}

// Checkout places a fixed commit in a managed workspace without discarding
// local modifications. Clone deliberately leaves checkout to this operation.
func (g *Client) Checkout(ctx context.Context, repo string, commit OID) error {
	if err := g.guardRepo(ctx, repo); err != nil {
		return err
	}
	if err := g.requireCommit(ctx, repo, commit); err != nil {
		return err
	}
	_, err := g.run(ctx, repo, nil, "checkout", "--detach", string(commit))
	return err
}
func (g *Client) requireCommit(ctx context.Context, repo string, o OID) error {
	if err := validIDs(o); err != nil {
		return err
	}
	_, err := g.run(ctx, repo, nil, "cat-file", "-e", string(o)+"^{commit}")
	return err
}

func (g *Client) IsAncestor(ctx context.Context, repo string, from, to OID) (bool, error) {
	if err := validIDs(from, to); err != nil {
		return false, err
	}
	if err := g.guardRepo(ctx, repo); err != nil {
		return false, err
	}
	for _, o := range []OID{from, to} {
		if err := g.requireCommit(ctx, repo, o); err != nil {
			return false, err
		}
	}
	_, err := g.run(ctx, repo, nil, "merge-base", "--is-ancestor", string(from), string(to))
	if err == nil {
		return true, nil
	}
	var command *Error
	if errors.As(err, &command) && command.ExitCode == 1 && !errors.Is(err, process.ErrTreeNotStopped) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return false, nil
	}
	return false, err
}

func (g *Client) CommitAll(ctx context.Context, repo, message string, id Identity) (OID, error) {
	if err := g.guardRepo(ctx, repo); err != nil {
		return "", err
	}
	env, err := identityEnv(id)
	if err != nil {
		return "", err
	}
	if _, err = g.run(ctx, repo, nil, "add", "--all", "--", ":/"); err != nil {
		return "", err
	}
	if _, err = g.run(ctx, repo, env, "commit", "--no-gpg-sign", "--no-verify", "-m", message); err != nil {
		return "", err
	}
	return g.Head(ctx, repo)
}

type Bundle struct {
	Base   OID
	Head   OID
	Ref    string
	SHA256 string
	Bytes  int64
}

func (g *Client) CreateBundle(ctx context.Context, repo string, b, h OID, dest string) (Bundle, error) {
	result := Bundle{Base: b, Head: h}
	if err := validIDs(b, h); err != nil {
		return result, err
	}
	ok, err := g.IsAncestor(ctx, repo, b, h)
	if err != nil {
		return result, err
	}
	if !ok {
		return result, ErrNotAncestor
	}
	if b == h {
		return result, ErrNoChanges
	}
	return g.createBundle(ctx, repo, b, h, dest)
}

// CreateSnapshotBundle contains the complete reachable history of H, with no
// prerequisite commits. It can initialize a fresh Runner repository.
func (g *Client) CreateSnapshotBundle(ctx context.Context, repo string, h OID, dest string) (Bundle, error) {
	if err := validIDs(h); err != nil {
		return Bundle{}, err
	}
	if err := g.guardRepo(ctx, repo); err != nil {
		return Bundle{}, err
	}
	if err := g.requireCommit(ctx, repo, h); err != nil {
		return Bundle{}, err
	}
	return g.createBundle(ctx, repo, h, h, dest)
}

func (g *Client) createBundle(ctx context.Context, repo string, b, h OID, dest string) (result Bundle, err error) {
	result = Bundle{Base: b, Head: h}
	var nonce [12]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return result, err
	}
	result.Ref = "refs/coordica/bundles/" + hex.EncodeToString(nonce[:])
	if _, err = g.run(ctx, repo, nil, "update-ref", result.Ref, string(h), strings.Repeat("0", len(h))); err != nil {
		return result, err
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if _, cleanupErr := g.run(cleanupCtx, repo, nil, "update-ref", "-d", result.Ref, string(h)); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("bundle reference cleanup: %w", cleanupErr))
		}
	}()
	tmp, err := os.MkdirTemp(filepath.Dir(dest), ".bundle-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(tmp)
	file := filepath.Join(tmp, "material.bundle")
	// Self-contained bundles avoid trusting undeclared prerequisite objects.
	if _, err = g.run(ctx, repo, nil, "bundle", "create", file, result.Ref); err != nil {
		return result, err
	}
	result.SHA256, result.Bytes, err = copyMaterial(file, dest)
	return result, err
}

func copyMaterial(source, dest string) (digest string, size int64, err error) {
	in, err := os.Open(source)
	if err != nil {
		return "", 0, err
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return "", 0, err
	}
	if !fi.Mode().IsRegular() || fi.Size() > MaxBundleBytes {
		return "", 0, ErrInvalidBundle
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", 0, err
	}
	success := false
	defer func() {
		out.Close()
		if !success {
			os.Remove(dest)
		}
	}()
	hash := sha256.New()
	size, err = io.Copy(io.MultiWriter(out, hash), io.LimitReader(in, MaxBundleBytes+1))
	if err != nil {
		return "", size, err
	}
	if size > MaxBundleBytes {
		return "", size, ErrInvalidBundle
	}
	if err = out.Sync(); err != nil {
		return "", size, err
	}
	if err = out.Close(); err != nil {
		return "", size, err
	}
	success = true
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

func (g *Client) ImportBundle(ctx context.Context, repo, file string, b, h OID) (Bundle, error) {
	return g.importBundle(ctx, repo, file, b, h, true)
}

// ImportSnapshotBundle verifies a complete snapshot in an initialized object
// store. Unlike a Run contribution, its H need not exist before import.
func (g *Client) ImportSnapshotBundle(ctx context.Context, repo, file string, h OID) (Bundle, error) {
	return g.importBundle(ctx, repo, file, h, h, false)
}

func (g *Client) importBundle(ctx context.Context, repo, file string, b, h OID, requireBase bool) (Bundle, error) {
	result := Bundle{Base: b, Head: h}
	if err := validIDs(b, h); err != nil {
		return result, err
	}
	if err := g.guardRepo(ctx, repo); err != nil {
		return result, err
	}
	// The application's issued baseline must already exist independently.
	if requireBase {
		if err := g.requireCommit(ctx, repo, b); err != nil {
			return result, fault("bundle", errors.Join(ErrInvalidBundle, err), "baseline unavailable")
		}
	}
	tmp, err := os.MkdirTemp("", "coordica-bundle-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(tmp)
	copy := filepath.Join(tmp, "input.bundle")
	result.SHA256, result.Bytes, err = copyMaterial(file, copy)
	if err != nil {
		return result, err
	}
	if _, err = g.run(ctx, repo, nil, "bundle", "verify", copy); err != nil {
		return result, fault("bundle", errors.Join(ErrInvalidBundle, err), "verification failed")
	}
	out, err := g.run(ctx, repo, nil, "bundle", "list-heads", copy)
	if err != nil {
		return result, err
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != 1 {
		return result, fault("bundle", ErrInvalidBundle, "exactly one declared head required")
	}
	fields := strings.Fields(lines[0])
	if len(fields) != 2 || fields[0] != string(h) || !strings.HasPrefix(fields[1], "refs/coordica/bundles/") {
		return result, fault("bundle", ErrInvalidBundle, "head or reference mismatch")
	}
	result.Ref = fields[1]
	if err = g.validRef(ctx, result.Ref); err != nil {
		return result, errors.Join(ErrInvalidBundle, err)
	}
	if _, err = g.run(ctx, repo, nil, "bundle", "unbundle", copy); err != nil {
		return result, fault("bundle", errors.Join(ErrInvalidBundle, err), "object import failed")
	}
	if err = g.requireCommit(ctx, repo, h); err != nil {
		return result, errors.Join(ErrInvalidBundle, err)
	}
	if _, err = g.run(ctx, repo, nil, "fsck", "--strict", "--no-reflogs", string(h)); err != nil {
		return result, fault("bundle", errors.Join(ErrInvalidBundle, err), "object integrity failed")
	}
	ok, err := g.IsAncestor(ctx, repo, b, h)
	if err != nil {
		return result, err
	}
	if !ok {
		return result, ErrNotAncestor
	}
	// Keep imported objects reachable for later independent clones and backup.
	// Never replace an existing material reference with a different object.
	out, existingErr := g.run(ctx, repo, nil, "rev-parse", "--verify", result.Ref)
	if errors.Is(existingErr, process.ErrTreeNotStopped) {
		return result, existingErr
	}
	if existingErr == nil {
		if strings.TrimSpace(string(out)) != string(h) {
			return result, ErrInvalidBundle
		}
	} else if _, err = g.run(ctx, repo, nil, "update-ref", result.Ref, string(h), strings.Repeat("0", len(h))); err != nil {
		return result, errors.Join(existingErr, err)
	}
	return result, nil
}

// CloneSnapshot verifies a complete bundle against H in a new quarantine
// repository before creating a fixed, clean Runner workspace.
func (g *Client) CloneSnapshot(ctx context.Context, file string, h OID, dest string) error {
	if err := validIDs(h); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "coordica-snapshot-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	stage := filepath.Join(tmp, "verified.git")
	if err = g.InitObjectFormat(ctx, stage, true, h); err != nil {
		return err
	}
	if _, err = g.importBundle(ctx, stage, file, h, h, false); err != nil {
		return err
	}
	if _, err = g.run(ctx, stage, nil, "update-ref", "refs/heads/main", string(h)); err != nil {
		return err
	}
	if err = g.Clone(ctx, stage, dest); err != nil {
		return err
	}
	if err = g.Checkout(ctx, dest, h); err != nil {
		return err
	}
	return g.VerifyClean(ctx, dest, h)
}

type Change struct {
	Status  string
	Score   int
	OldPath string
	NewPath string
	OldMode string
	NewMode string
	OldOID  OID
	NewOID  OID
}

func (c Change) Paths() []string {
	var p []string
	if c.OldPath != "" {
		p = append(p, c.OldPath)
	}
	if c.NewPath != "" && c.NewPath != c.OldPath {
		p = append(p, c.NewPath)
	}
	return p
}
func (c Change) Unsupported() bool {
	for _, m := range []string{c.OldMode, c.NewMode} {
		if m != "000000" && m != "100644" && m != "100755" {
			return true
		}
	}
	return false
}

func (g *Client) DiffPaths(ctx context.Context, repo string, from, to OID) ([]Change, error) {
	if err := validIDs(from, to); err != nil {
		return nil, err
	}
	if err := g.guardRepo(ctx, repo); err != nil {
		return nil, err
	}
	for _, o := range []OID{from, to} {
		if err := g.requireCommit(ctx, repo, o); err != nil {
			return nil, err
		}
	}
	out, err := g.run(ctx, repo, nil, "diff", "--raw", "-z", "--no-abbrev", "--no-ext-diff", "--no-textconv", "--find-renames=50%", string(from), string(to), "--")
	if err != nil {
		return nil, err
	}
	return parseRawDiff(out)
}

// DiffPatch returns the complete fixed-object patch, including binary data,
// without running external diff tools, filters, or text conversion. Oversized
// output returns ErrOutputLimit, never a successful truncated review input.
func (g *Client) DiffPatch(ctx context.Context, repo string, from, to OID) ([]byte, error) {
	if err := validIDs(from, to); err != nil {
		return nil, err
	}
	if err := g.guardRepo(ctx, repo); err != nil {
		return nil, err
	}
	for _, oid := range []OID{from, to} {
		if err := g.requireCommit(ctx, repo, oid); err != nil {
			return nil, err
		}
	}
	return g.run(ctx, repo, nil, "diff", "--patch", "--binary", "--full-index", "--no-ext-diff", "--no-textconv", "--find-renames=50%", string(from), string(to), "--")
}

func parseRawDiff(raw []byte) ([]Change, error) {
	parts := bytes.Split(raw, []byte{0})
	var changes []Change
	for i := 0; i < len(parts)-1; {
		fields := strings.Fields(string(parts[i]))
		i++
		if len(fields) != 5 || !strings.HasPrefix(fields[0], ":") || len(fields[4]) == 0 {
			return nil, fault("diff", ErrGit, "malformed raw record")
		}
		c := Change{OldMode: strings.TrimPrefix(fields[0], ":"), NewMode: fields[1], OldOID: OID(fields[2]), NewOID: OID(fields[3]), Status: fields[4][:1]}
		if len(fields[4]) > 1 {
			var err error
			c.Score, err = strconv.Atoi(fields[4][1:])
			if err != nil {
				return nil, ErrGit
			}
		}
		if i >= len(parts)-1 {
			return nil, ErrGit
		}
		first := string(parts[i])
		i++
		switch c.Status {
		case "A":
			c.NewPath = first
		case "D":
			c.OldPath = first
		case "R", "C":
			c.OldPath = first
			if i >= len(parts)-1 {
				return nil, ErrGit
			}
			c.NewPath = string(parts[i])
			i++
		case "M", "T":
			c.OldPath = first
			c.NewPath = first
		default:
			return nil, fault("diff", ErrGit, "unsupported diff status")
		}
		for _, p := range c.Paths() {
			if err := ValidatePath(p); err != nil {
				return nil, fault("diff", ErrUnsupportedObject, "unsupported repository path")
			}
		}
		changes = append(changes, c)
	}
	return changes, nil
}

type DiffResult struct {
	Changes     []Change
	Scope       ScopeResult
	Unsupported []Change
}

func (g *Client) CheckScope(ctx context.Context, repo string, from, to OID, scope *Scope) (DiffResult, error) {
	var result DiffResult
	if scope == nil {
		return result, fmt.Errorf("scope required")
	}
	changes, err := g.DiffPaths(ctx, repo, from, to)
	if err != nil {
		return result, err
	}
	result.Changes = changes
	var paths []string
	for _, c := range changes {
		paths = append(paths, c.Paths()...)
		if c.Unsupported() {
			result.Unsupported = append(result.Unsupported, c)
		}
	}
	result.Scope = scope.Check(paths)
	if !result.Scope.Allowed {
		return result, ErrOutOfScope
	}
	if len(result.Unsupported) > 0 {
		return result, ErrUnsupportedObject
	}
	return result, nil
}

type Candidate struct {
	Main      OID
	Head      OID
	Commit    OID
	Workspace string
}

func (g *Client) BuildCandidate(ctx context.Context, repo string, m, h OID, workspace string, id Identity) (Candidate, error) {
	c := Candidate{Main: m, Head: h, Workspace: workspace}
	if err := validIDs(m, h); err != nil {
		return c, err
	}
	env, err := identityEnv(id)
	if err != nil {
		return c, err
	}
	already, err := g.IsAncestor(ctx, repo, h, m)
	if err != nil {
		return c, err
	}
	if already {
		return c, ErrNoChanges
	}
	if err = g.Clone(ctx, repo, workspace); err != nil {
		return c, err
	}
	// Custom material refs are not in clone's default branch refspec. Fetch the
	// two exact advertised objects explicitly before checkout/merge.
	source, err := filepath.Abs(repo)
	if err != nil {
		return c, err
	}
	if _, err = g.run(ctx, workspace, nil, "fetch", "--no-tags", "--no-recurse-submodules", "--", source, string(m)+":refs/coordica/input/main", string(h)+":refs/coordica/input/head"); err != nil {
		return c, err
	}
	if _, err = g.run(ctx, workspace, nil, "checkout", "--detach", string(m)); err != nil {
		return c, err
	}
	if _, err = g.run(ctx, workspace, env, "merge", "--no-ff", "--no-commit", "--no-edit", string(h)); err != nil {
		if errors.Is(err, process.ErrTreeNotStopped) {
			return c, err
		}
		if errors.Is(err, ErrGit) {
			unmerged, e := g.run(ctx, workspace, nil, "ls-files", "--unmerged", "-z")
			if e != nil {
				return c, errors.Join(err, e)
			}
			if e == nil && len(unmerged) > 0 {
				return c, fault("merge", errors.Join(ErrConflict, err), "candidate merge did not complete")
			}
		}
		return c, err
	}
	tree, err := g.run(ctx, workspace, nil, "write-tree")
	if err != nil {
		return c, err
	}
	out, err := g.run(ctx, workspace, env, "commit-tree", strings.TrimSpace(string(tree)), "-p", string(m), "-p", string(h), "-m", "Coordica: merge fixed candidate")
	if err != nil {
		return c, err
	}
	c.Commit, err = ParseOID(strings.TrimSpace(string(out)))
	if err != nil {
		return c, err
	}
	if _, err = g.run(ctx, workspace, nil, "reset", "--hard", string(c.Commit)); err != nil {
		return c, err
	}
	if err = g.VerifyClean(ctx, workspace, c.Commit); err != nil {
		return c, err
	}
	// Preserve the exact commit in the center object store as well as its fixed
	// workspace. The ref name is derived from the commit, never a mutable task.
	if _, err = g.run(ctx, repo, nil, "fetch", "--no-tags", "--no-recurse-submodules", "--", workspace, string(c.Commit)+":refs/coordica/candidates/"+string(c.Commit)); err != nil {
		return c, err
	}
	return c, nil
}

// VerifyClean rejects staged, tracked, untracked and ignored inputs. Build
// outputs must live outside this fixed source directory.
func (g *Client) VerifyClean(ctx context.Context, workspace string, c OID) error {
	return g.VerifyCheckOutputs(ctx, workspace, c, nil)
}

// VerifyCheckOutputs permits explicitly declared untracked/ignored outputs
// only after trusted checks. Call VerifyClean before executing any checks.
// HEAD, tracked blobs, modes and the index must still match the fixed commit.
func (g *Client) VerifyCheckOutputs(ctx context.Context, workspace string, c OID, outputs []string) error {
	allowed, err := NewScope(outputs, nil)
	if err != nil {
		return err
	}
	if err := validIDs(c); err != nil {
		return err
	}
	h, err := g.Head(ctx, workspace)
	if err != nil {
		return err
	}
	if h != c {
		return fault("verify", ErrDirty, "HEAD differs")
	}
	out, err := g.run(ctx, workspace, nil, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignored=matching")
	if err != nil {
		return err
	}
	for _, record := range bytes.Split(out, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		if len(record) < 4 || (string(record[:2]) != "??" && string(record[:2]) != "!!") || record[2] != ' ' || !allowed.Check([]string{strings.TrimSuffix(string(record[3:]), "/")}).Allowed {
			return fault("verify", ErrDirty, "tracked changes or undeclared check outputs present")
		}
	}
	// Unsupported tree entries cannot become invisible behind a clean status.
	out, err = g.run(ctx, workspace, nil, "ls-tree", "-r", "--full-tree", "-z", string(c))
	if err != nil {
		return err
	}
	for _, record := range bytes.Split(out, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		pair := bytes.SplitN(record, []byte{'\t'}, 2)
		if len(pair) != 2 {
			return ErrGit
		}
		fields := strings.Fields(string(pair[0]))
		if len(fields) != 3 {
			return ErrGit
		}
		if (fields[0] != "100644" && fields[0] != "100755") || fields[1] != "blob" {
			return ErrUnsupportedObject
		}
		p := string(pair[1])
		if err = ValidatePath(p); err != nil {
			return ErrUnsupportedObject
		}
		if err = verifyBlob(filepath.Join(workspace, filepath.FromSlash(p)), fields[2], fields[0]); err != nil {
			return err
		}
	}
	return nil
}

func verifyBlob(file, expected, gitMode string) error {
	fi, err := os.Lstat(file)
	if err != nil || !fi.Mode().IsRegular() {
		return fault("verify", ErrDirty, "source file missing or not regular")
	}
	// Windows does not represent Git's executable bit in ordinary file modes.
	// On Unix, do not let core.fileMode=false hide a changed executable input.
	if runtime.GOOS != "windows" && (fi.Mode().Perm()&0100 != 0) != (gitMode == "100755") {
		return fault("verify", ErrDirty, "source executable mode differs")
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	var digest []byte
	if len(expected) == 40 {
		hash := sha1.New()
		fmt.Fprintf(hash, "blob %d\x00", fi.Size())
		if _, err = io.Copy(hash, f); err != nil {
			return err
		}
		digest = hash.Sum(nil)
	} else if len(expected) == 64 {
		hash := sha256.New()
		fmt.Fprintf(hash, "blob %d\x00", fi.Size())
		if _, err = io.Copy(hash, f); err != nil {
			return err
		}
		digest = hash.Sum(nil)
	} else {
		return ErrInvalidOID
	}
	if hex.EncodeToString(digest) != expected {
		return fault("verify", ErrDirty, "source content differs")
	}
	return nil
}

func (g *Client) validRef(ctx context.Context, ref string) error {
	if !strings.HasPrefix(ref, "refs/heads/") && !strings.HasPrefix(ref, "refs/coordica/") {
		return ErrInvalidRef
	}
	if strings.ContainsAny(ref, "\x00\r\n") {
		return ErrInvalidRef
	}
	_, err := g.run(ctx, "", nil, "check-ref-format", ref)
	if err != nil {
		return errors.Join(ErrInvalidRef, err)
	}
	return nil
}
func (g *Client) bareRemote(ctx context.Context, remote string) (string, error) {
	dir, err := localDir(remote)
	if err != nil {
		return "", err
	}
	if err = g.guardRepo(ctx, dir); err != nil {
		return "", err
	}
	out, err := g.run(ctx, dir, nil, "rev-parse", "--is-bare-repository")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(string(out)) != "true" {
		return "", fmt.Errorf("remote must be local bare repository")
	}
	return dir, nil
}

func (g *Client) Query(ctx context.Context, remote, ref string) (OID, error) {
	if err := g.validRef(ctx, ref); err != nil {
		return "", err
	}
	if strings.Contains(remote, "://") {
		u, err := httpsRemote(remote)
		if err != nil {
			return "", err
		}
		out, err := g.run(ctx, "", g.authEnv(u), "ls-remote", "--refs", "--exit-code", "--", u, ref)
		if err != nil {
			return "", err
		}
		fields := strings.Fields(string(out))
		if len(fields) != 2 || fields[1] != ref {
			return "", ErrInvalidRef
		}
		return ParseOID(fields[0])
	}
	dir, err := g.bareRemote(ctx, remote)
	if err != nil {
		return "", err
	}
	out, err := g.run(ctx, dir, nil, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	return ParseOID(strings.TrimSpace(string(out)))
}

// Fetch fixes the remote ref observed by the fetch itself, retaining its
// objects under a unique local ref. It never overwrites a task or main ref.
func (g *Client) Fetch(ctx context.Context, repo, remote, ref string) (OID, error) {
	if err := g.guardRepo(ctx, repo); err != nil {
		return "", err
	}
	if err := g.validRef(ctx, ref); err != nil {
		return "", err
	}
	var source string
	var err error
	if strings.Contains(remote, "://") {
		source, err = httpsRemote(remote)
	} else {
		source, err = g.bareRemote(ctx, remote)
	}
	if err != nil {
		return "", err
	}
	var nonce [12]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return "", err
	}
	local := "refs/coordica/fetch/" + hex.EncodeToString(nonce[:])
	if _, err = g.run(ctx, repo, g.authEnv(source), "fetch", "--no-tags", "--no-recurse-submodules", "--", source, ref+":"+local); err != nil {
		return "", err
	}
	out, err := g.run(ctx, repo, nil, "rev-parse", "--verify", local+"^{commit}")
	if err != nil {
		return "", err
	}
	return ParseOID(strings.TrimSpace(string(out)))
}

type PublishResult struct {
	Expected  OID
	Candidate OID
	Observed  OID
}

// Publish enforces both fast-forward ancestry and an explicit expected-OID
// lease. An uncertain result is always returned for the caller to reconcile.
func (g *Client) Publish(ctx context.Context, repo, remote, ref string, m, c OID) (PublishResult, error) {
	r := PublishResult{Expected: m, Candidate: c}
	if err := validIDs(m, c); err != nil {
		return r, err
	}
	if err := g.validRef(ctx, ref); err != nil {
		return r, err
	}
	var dir string
	var err error
	if strings.Contains(remote, "://") {
		dir, err = httpsRemote(remote)
	} else {
		dir, err = g.bareRemote(ctx, remote)
	}
	if err != nil {
		return r, err
	}
	ok, err := g.IsAncestor(ctx, repo, m, c)
	if err != nil {
		return r, err
	}
	if !ok {
		return r, ErrNotAncestor
	}
	r.Observed, err = g.Query(ctx, dir, ref)
	if err != nil {
		return r, err
	}
	if r.Observed != m {
		return r, ErrCAS
	}
	pushOut, pushErr := g.run(ctx, repo, g.authEnv(dir), "push", "--porcelain", "--no-verify", "--force-with-lease="+ref+":"+string(m), "--", dir, string(c)+":"+ref)
	if ctx.Err() != nil {
		return r, fault("publish", errors.Join(ErrUnknown, pushErr, ctx.Err()), "context ended; query remote before retry")
	}
	r.Observed, err = g.Query(ctx, dir, ref)
	if err != nil {
		return r, fault("publish", errors.Join(ErrUnknown, pushErr, err), "remote observation unavailable")
	}
	if pushErr == nil && r.Observed == c {
		return r, nil
	}
	if pushErr != nil && strings.Contains(string(pushOut), "(stale info)") {
		return r, errors.Join(ErrCAS, pushErr)
	}
	if pushErr != nil && r.Observed == m {
		return r, pushErr
	}
	return r, fault("publish", errors.Join(ErrUnknown, pushErr), "query intent and remote before retry")
}
