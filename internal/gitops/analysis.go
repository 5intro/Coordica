package gitops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

var ErrUnborn = errors.New("fixed repository has no commit yet")

type SourceFile struct {
	Path string `json:"path"`
	Text string `json:"text"`
}
type AnalysisSnapshot struct {
	SHA       string       `json:"sha"`
	Files     []SourceFile `json:"files"`
	Omissions []string     `json:"omissions"`
}

// AnalysisRevision reads the explicitly registered local repository branch.
// No checkout, fetch, hooks, scripts, ignored working files or remote I/O.
func (g *Client) AnalysisRevision(ctx context.Context, repo, branch string) (string, error) {
	if !filepath.IsAbs(repo) {
		return "", fmt.Errorf("analysis requires a fixed absolute local repository")
	}
	if _, e := os.Lstat(filepath.Join(repo, ".git")); os.IsNotExist(e) {
		if _, bare := os.Stat(filepath.Join(repo, "HEAD")); os.IsNotExist(bare) {
			return "", ErrUnborn
		}
	}
	if err := g.guardRepo(ctx, repo); err != nil {
		return "", err
	}
	if err := g.validRef(ctx, "refs/heads/"+branch); err != nil {
		return "", err
	}
	out, err := g.run(ctx, repo, nil, "rev-parse", "--verify", "refs/heads/"+branch+"^{commit}")
	if err != nil {
		commits, e := g.run(ctx, repo, nil, "rev-list", "--all", "--max-count=1")
		if e != nil {
			return "", errors.Join(err, e)
		}
		if strings.TrimSpace(string(commits)) == "" {
			return "", ErrUnborn
		}
		return "", err
	}
	id, err := ParseOID(strings.TrimSpace(string(out)))
	return string(id), err
}

func analysisPrivate(path string) bool {
	for _, part := range strings.Split(strings.ToLower(path), "/") {
		if part == "docs" || part == "node_modules" || part == "dist" || part == "third_party" || part == "vendor" || part == ".git" || part == ".aws" || part == ".ssh" || part == ".codex" || part == ".agents" || part == ".cache" || strings.HasPrefix(part, ".coordica") || strings.HasPrefix(part, ".env") {
			return true
		}
	}
	name := strings.ToLower(filepath.Base(path))
	if strings.HasSuffix(name, ".generated.ts") || strings.HasSuffix(name, ".schema.json") || name == "package-lock.json" || name == "go.sum" {
		return true
	}
	return strings.HasSuffix(name, ".key") || strings.HasSuffix(name, ".pem") || strings.HasSuffix(name, ".token") || strings.HasSuffix(name, ".db") || strings.HasSuffix(name, ".sqlite3") || name == "credentials.json" || name == "credentials.toml" || strings.HasPrefix(name, "secrets.") || name == "runner.json" || name == "interaction.json" || name == "host.json"
}

// ReadAnalysis supplies bounded public tracked blobs from an immutable tree.
// Symlinks and submodules are omissions; no blob is interpreted as a command.
func (g *Client) ReadAnalysis(ctx context.Context, repo, sha string) (AnalysisSnapshot, error) {
	s := AnalysisSnapshot{SHA: sha, Files: []SourceFile{}, Omissions: []string{"ignored and untracked files are excluded"}}
	id, err := ParseOID(sha)
	if err != nil {
		return s, err
	}
	if err = g.guardRepo(ctx, repo); err != nil {
		return s, err
	}
	out, err := g.run(ctx, repo, nil, "ls-tree", "-r", "-z", string(id))
	if err != nil {
		return s, err
	}
	entries := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	if len(entries) > 10000 {
		return s, ErrOutputLimit
	}
	total := 0
	for _, entry := range entries {
		if entry == "" {
			continue
		}
		header, path, ok := strings.Cut(entry, "\t")
		fields := strings.Fields(header)
		if !ok || len(fields) != 3 || strings.ContainsAny(path, "\x00\r\n:") || ValidatePath(path) != nil || path == "." || strings.HasPrefix(path, "/") || strings.Contains(path, "\\") || strings.Contains("/"+path+"/", "/../") {
			return s, ErrUnsupportedObject
		}
		physicalRepo := repo
		if resolved, e := filepath.EvalSymlinks(repo); e == nil {
			physicalRepo = resolved
		}
		private := false
		full := filepath.Join(physicalRepo, filepath.FromSlash(path))
		for _, excluded := range g.analysisPrivatePaths {
			if resolved, e := filepath.EvalSymlinks(excluded); e == nil {
				excluded = resolved
			}
			rel, e := filepath.Rel(excluded, full)
			if e == nil && (rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
				private = true
			}
		}
		if private || analysisPrivate(path) || fields[0] != "100644" && fields[0] != "100755" {
			s.Omissions = append(s.Omissions, path+": private, generated or unsupported")
			continue
		}
		size, e := g.run(ctx, repo, nil, "cat-file", "-s", fields[2])
		if e != nil {
			return s, e
		}
		var n int
		if _, e = fmt.Sscanf(string(size), "%d", &n); e != nil {
			return s, e
		}
		if n > 512<<10 || total+n > 16<<20 {
			return s, fmt.Errorf("relevant source exceeds analysis limit at %s; cannot claim readiness", path)
		}
		blob, e := g.run(ctx, repo, nil, "cat-file", "blob", fields[2])
		if e != nil {
			return s, e
		}
		if !utf8.Valid(blob) || strings.ContainsRune(string(blob), 0) {
			s.Omissions = append(s.Omissions, path+": binary")
			continue
		}
		total += len(blob)
		s.Files = append(s.Files, SourceFile{Path: path, Text: string(blob)})
	}
	if len(s.Files) == 0 {
		return s, fmt.Errorf("no public tracked source to analyze")
	}
	return s, nil
}

// WithAnalysisPrivatePaths excludes trusted local role assets even if a user
// accidentally tracks them. Paths are selected by setup, never by model calls.
func (g *Client) WithAnalysisPrivatePaths(paths []string) *Client {
	copy := *g
	copy.analysisPrivatePaths = append([]string(nil), paths...)
	return &copy
}
