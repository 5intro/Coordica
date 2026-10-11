package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/store"
)

const backupVersion = 1

type backupFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}
type backupGit struct {
	OID  string     `json:"oid"`
	File backupFile `json:"file"`
}
type backupManifest struct {
	Version   int          `json:"version"`
	CreatedAt time.Time    `json:"created_at"`
	Database  backupFile   `json:"database"`
	Artifacts []backupFile `json:"artifacts"`
	Git       []backupGit  `json:"git"`
}
type backupRecords struct {
	Artifacts          map[string]domain.Artifact
	CandidateArtifacts map[string]domain.Artifact
	Candidates         []domain.Candidate
	Runs               []domain.Run
	Intents            []domain.Intent
}

// Backup holds the same Git lock as preparation/publication. Any future
// material collector must honor this barrier before deleting referenced files.
func (w *Integrator) Backup(ctx context.Context, a Actor) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.Service.backup(ctx, a, w.Git)
}

// Backup is for a stopped center or a caller already holding its Integrator
// barrier. A running HTTP center calls Integrator.Backup instead.
func (s *Service) Backup(ctx context.Context, a Actor) (string, error) {
	return s.backup(ctx, a, gitops.New(""))
}

func (s *Service) backup(ctx context.Context, a Actor, g *gitops.Client) (string, error) {
	if err := owner(a); err != nil {
		return "", err
	}
	if g == nil {
		g = gitops.New("")
	}
	parent := filepath.Join(s.Dir, "backups")
	if err := os.MkdirAll(parent, 0700); err != nil {
		return "", err
	}
	if err := os.Chmod(parent, 0700); err != nil {
		return "", err
	}
	id := ID("backup_")
	stage := filepath.Join(parent, ".pending-"+id)
	final := filepath.Join(parent, id)
	if err := os.Mkdir(stage, 0700); err != nil {
		return "", err
	}
	published := false
	defer func() {
		if !published {
			os.RemoveAll(stage)
		}
	}()
	database := filepath.Join(stage, "coordica.db")
	// SQLite creates a consistent independent file. The active db/WAL files are
	// never copied; records below are read from this exact snapshot.
	if err := s.DB.Backup(ctx, database); err != nil {
		return "", err
	}
	records, err := readBackupRecords(ctx, database, true)
	if err != nil {
		return "", err
	}
	manifest := backupManifest{Version: backupVersion, CreatedAt: s.Now()}
	manifest.Database, err = describeBackupFile(ctx, stage, "coordica.db")
	if err != nil {
		return "", err
	}
	if f, e := os.OpenFile(database, os.O_RDWR, 0600); e != nil {
		return "", e
	} else {
		e = f.Sync()
		closeErr := f.Close()
		if e != nil {
			return "", e
		}
		if closeErr != nil {
			return "", closeErr
		}
	}
	ids := make([]string, 0, len(records.Artifacts))
	for id := range records.Artifacts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		artifact := records.Artifacts[id]
		if err = gitops.ValidatePath(id); err != nil || strings.Contains(id, "/") {
			return "", fmt.Errorf("invalid artifact identifier")
		}
		rel := "artifacts/" + id
		copied, e := copyBackupFile(ctx, filepath.Join(s.Dir, "artifacts", id), stage, rel)
		if e != nil {
			return "", e
		}
		if copied.SHA256 != artifact.SHA256 || copied.Size != artifact.Size {
			return "", fmt.Errorf("artifact %s failed immutable digest verification", id)
		}
		manifest.Artifacts = append(manifest.Artifacts, copied)
	}
	oids, err := requiredBackupOIDs(records)
	if err != nil {
		return "", err
	}
	if len(oids) > 0 {
		quarantine := filepath.Join(stage, ".git-materials")
		if err = g.Clone(ctx, filepath.Join(s.Dir, "repository"), quarantine); err != nil {
			return "", fmt.Errorf("backup Git object store: %w", err)
		}
		// An accepted H may not yet have been imported by the integration worker.
		// Material is imported only into this offline quarantine, after its digest
		// was checked against the database snapshot.
		for _, c := range records.Candidates {
			if c.Head == "" || (c.State == "rejected" && c.Commit == "") {
				continue
			}
			if _, e := g.IsAncestor(ctx, quarantine, gitops.OID(c.Head), gitops.OID(c.Head)); e == nil {
				continue
			}
			artifact, ok := records.CandidateArtifacts[c.ID]
			if !ok {
				return "", fmt.Errorf("candidate %s missing code material", c.ID)
			}
			if _, e := g.ImportBundle(ctx, quarantine, filepath.Join(stage, "artifacts", artifact.ID), gitops.OID(c.Base), gitops.OID(c.Head)); e != nil {
				return "", fmt.Errorf("candidate %s material: %w", c.ID, e)
			}
		}
		if err = os.MkdirAll(filepath.Join(stage, "git"), 0700); err != nil {
			return "", err
		}
		for _, oid := range oids {
			rel := "git/" + string(oid) + ".bundle"
			bundle, e := g.CreateSnapshotBundle(ctx, quarantine, oid, filepath.Join(stage, filepath.FromSlash(rel)))
			if e != nil {
				return "", fmt.Errorf("required Git object %s unavailable: %w", oid, e)
			}
			manifest.Git = append(manifest.Git, backupGit{OID: string(oid), File: backupFile{Path: rel, SHA256: bundle.SHA256, Size: bundle.Bytes}})
		}
		if err = os.RemoveAll(quarantine); err != nil {
			return "", err
		}
	}
	// Only a complete closure receives a manifest and becomes visible as a
	// successful backup. No owner.token or executable Git config is included.
	if err = writeBackupManifest(stage, manifest); err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if err = os.Rename(stage, final); err != nil {
		return "", err
	}
	published = true
	return final, nil
}

// readBackupRecords optionally removes authentication material from the
// offline copy, including cached registration responses in idempotency.
func readBackupRecords(ctx context.Context, file string, sanitize bool) (backupRecords, error) {
	result := backupRecords{Artifacts: map[string]domain.Artifact{}, CandidateArtifacts: map[string]domain.Artifact{}}
	db, err := sql.Open("sqlite", file)
	if err != nil {
		return result, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if sanitize {
		// These are standalone offline files. Secure deletion and compaction
		// prevent revoked secrets surviving in freed SQLite pages.
		if _, err = db.ExecContext(ctx, "PRAGMA journal_mode=DELETE; PRAGMA secure_delete=ON; DELETE FROM idempotency; DELETE FROM records WHERE kind IN ('credential','pairing'); VACUUM;"); err != nil {
			return result, err
		}
	}
	rows, err := db.QueryContext(ctx, "SELECT kind,id,data FROM records WHERE kind IN ('artifact','candidate_artifact','candidate','run','intent') ORDER BY kind,id")
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, id string
		var b []byte
		if err = rows.Scan(&kind, &id, &b); err != nil {
			return result, err
		}
		switch kind {
		case "artifact", "candidate_artifact":
			var a domain.Artifact
			if err = json.Unmarshal(b, &a); err != nil {
				return result, err
			}
			if a.ID == "" {
				return result, fmt.Errorf("artifact record has no identifier")
			}
			if old, ok := result.Artifacts[a.ID]; ok && (old.SHA256 != a.SHA256 || old.Size != a.Size) {
				return result, fmt.Errorf("artifact records disagree")
			}
			result.Artifacts[a.ID] = a
			if kind == "candidate_artifact" {
				result.CandidateArtifacts[id] = a
			}
		case "candidate":
			var c domain.Candidate
			if err = json.Unmarshal(b, &c); err != nil {
				return result, err
			}
			result.Candidates = append(result.Candidates, c)
		case "run":
			var r domain.Run
			if err = json.Unmarshal(b, &r); err != nil {
				return result, err
			}
			result.Runs = append(result.Runs, r)
		case "intent":
			var in domain.Intent
			if err = json.Unmarshal(b, &in); err != nil {
				return result, err
			}
			result.Intents = append(result.Intents, in)
		}
	}
	return result, rows.Err()
}

func requiredBackupOIDs(records backupRecords) ([]gitops.OID, error) {
	set := map[gitops.OID]bool{}
	add := func(raw string) error {
		if raw == "" {
			return nil
		}
		oid, err := gitops.ParseOID(raw)
		if err != nil {
			return fmt.Errorf("invalid required Git object: %w", err)
		}
		set[oid] = true
		return nil
	}
	for _, r := range records.Runs {
		if err := add(r.Base); err != nil {
			return nil, err
		}
	}
	for _, c := range records.Candidates {
		for _, oid := range []string{c.Base, c.Main, c.Commit} {
			if err := add(oid); err != nil {
				return nil, err
			}
		}
		if c.State != "rejected" || c.Commit != "" {
			if err := add(c.Head); err != nil {
				return nil, err
			}
		}
	}
	for _, in := range records.Intents {
		for _, oid := range []string{in.Expected, in.Desired} {
			if err := add(oid); err != nil {
				return nil, err
			}
		}
	}
	oids := make([]gitops.OID, 0, len(set))
	for oid := range set {
		oids = append(oids, oid)
	}
	sort.Slice(oids, func(i, j int) bool { return oids[i] < oids[j] })
	return oids, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func describeBackupFile(ctx context.Context, root, rel string) (backupFile, error) {
	f := backupFile{Path: rel}
	path, err := safeBackupFile(root, rel)
	if err != nil {
		return f, err
	}
	input, err := os.Open(path)
	if err != nil {
		return f, err
	}
	defer input.Close()
	h := sha256.New()
	f.Size, err = io.Copy(h, contextReader{ctx, input})
	f.SHA256 = hex.EncodeToString(h.Sum(nil))
	return f, err
}

func safeBackupFile(root, rel string) (string, error) {
	if err := gitops.ValidatePath(rel); err != nil {
		return "", fmt.Errorf("invalid backup path: %w", err)
	}
	current := root
	for i, part := range strings.Split(rel, "/") {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("backup cannot contain symlinks")
		}
		if i == len(strings.Split(rel, "/"))-1 {
			if !info.Mode().IsRegular() {
				return "", fmt.Errorf("backup material is not regular")
			}
		} else if !info.IsDir() {
			return "", fmt.Errorf("backup parent is not a directory")
		}
	}
	return current, nil
}

func copyBackupFile(ctx context.Context, source, root, rel string) (backupFile, error) {
	f := backupFile{Path: rel}
	if err := gitops.ValidatePath(rel); err != nil {
		return f, err
	}
	info, err := os.Lstat(source)
	if err != nil {
		return f, err
	}
	if !info.Mode().IsRegular() {
		return f, fmt.Errorf("backup source is not a regular immutable file")
	}
	dest := filepath.Join(root, filepath.FromSlash(rel))
	if err = os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return f, err
	}
	input, err := os.Open(source)
	if err != nil {
		return f, err
	}
	defer input.Close()
	output, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return f, err
	}
	defer output.Close()
	h := sha256.New()
	f.Size, err = io.Copy(io.MultiWriter(output, h), contextReader{ctx, input})
	if err != nil {
		return f, err
	}
	if err = output.Sync(); err != nil {
		return f, err
	}
	if err = output.Close(); err != nil {
		return f, err
	}
	f.SHA256 = hex.EncodeToString(h.Sum(nil))
	return f, nil
}

func writeBackupManifest(dir string, m backupManifest) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "manifest.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err = f.Write(append(b, '\n')); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	return f.Close()
}

// RestoreBackup creates a new offline data directory. It never writes into a
// running center's existing directory and never contacts a Git remote.
func RestoreBackup(ctx context.Context, backupDir, destination string) error {
	return restoreBackup(ctx, backupDir, destination, gitops.New(""))
}

// Restore allows the command entry point to use its selected Git binary.
func Restore(ctx context.Context, backupDir, destination string, g *gitops.Client) error {
	if g == nil {
		g = gitops.New("")
	}
	return restoreBackup(ctx, backupDir, destination, g)
}

func restoreBackup(ctx context.Context, backupDir, destination string, g *gitops.Client) error {
	manifestPath, err := safeBackupFile(backupDir, "manifest.json")
	if err != nil {
		return err
	}
	bytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var manifest backupManifest
	if err = json.Unmarshal(bytes, &manifest); err != nil {
		return err
	}
	if manifest.Version != backupVersion || manifest.Database.Path != "coordica.db" {
		return fmt.Errorf("unsupported backup manifest")
	}
	all := []backupFile{manifest.Database}
	for _, f := range manifest.Artifacts {
		if !strings.HasPrefix(f.Path, "artifacts/") || strings.Contains(strings.TrimPrefix(f.Path, "artifacts/"), "/") {
			return fmt.Errorf("invalid artifact backup entry")
		}
	}
	for _, item := range manifest.Git {
		oid, e := gitops.ParseOID(item.OID)
		if e != nil || item.File.Path != "git/"+string(oid)+".bundle" {
			return fmt.Errorf("invalid Git backup entry")
		}
	}
	all = append(all, manifest.Artifacts...)
	for _, item := range manifest.Git {
		all = append(all, item.File)
	}
	seen := map[string]bool{}
	for _, f := range all {
		if seen[f.Path] || f.Size < 0 || len(f.SHA256) != 64 {
			return fmt.Errorf("invalid or duplicate backup entry")
		}
		seen[f.Path] = true
		actual, e := describeBackupFile(ctx, backupDir, f.Path)
		if e != nil {
			return e
		}
		if actual.SHA256 != f.SHA256 || actual.Size != f.Size {
			return fmt.Errorf("backup checksum mismatch for %s", f.Path)
		}
	}
	// Reserve the destination atomically instead of overwriting any existing
	// directory during a final rename.
	if err = os.Mkdir(destination, 0700); err != nil {
		return err
	}
	success := false
	defer func() {
		if !success {
			os.RemoveAll(destination)
		}
	}()
	for _, f := range all {
		source, e := safeBackupFile(backupDir, f.Path)
		if e != nil {
			return e
		}
		copied, e := copyBackupFile(ctx, source, destination, f.Path)
		if e != nil {
			return e
		}
		if copied.SHA256 != f.SHA256 || copied.Size != f.Size {
			return fmt.Errorf("backup changed during restore")
		}
	}
	records, err := readBackupRecords(ctx, filepath.Join(destination, "coordica.db"), true)
	if err != nil {
		return err
	}
	for id, a := range records.Artifacts {
		rel := "artifacts/" + id
		if !seen[rel] {
			return fmt.Errorf("backup missing referenced artifact %s", id)
		}
		actual, e := describeBackupFile(ctx, destination, rel)
		if e != nil || actual.SHA256 != a.SHA256 || actual.Size != a.Size {
			return fmt.Errorf("restored artifact closure is inconsistent")
		}
	}
	oids, err := requiredBackupOIDs(records)
	if err != nil {
		return err
	}
	indexed := map[string]backupGit{}
	for _, item := range manifest.Git {
		oid, e := gitops.ParseOID(item.OID)
		if e != nil || item.File.Path != "git/"+string(oid)+".bundle" {
			return fmt.Errorf("invalid Git backup entry")
		}
		indexed[item.OID] = item
	}
	repo := filepath.Join(destination, "repository")
	if len(oids) > 0 {
		if err = g.InitObjectFormat(ctx, repo, false, oids[0]); err != nil {
			return err
		}
	}
	for _, oid := range oids {
		item, ok := indexed[string(oid)]
		if !ok {
			return fmt.Errorf("backup missing required Git object %s", oid)
		}
		if _, err = g.ImportSnapshotBundle(ctx, repo, filepath.Join(destination, filepath.FromSlash(item.File.Path)), oid); err != nil {
			return fmt.Errorf("restore Git object %s: %w", oid, err)
		}
	}
	// The restored repository is an offline object cache. A later Base() fetch
	// reads the real main, while pending candidates retain their exact objects.
	db, err := store.Open(filepath.Join(destination, "coordica.db"))
	if err != nil {
		return err
	}
	service := New(db, destination)
	if err = freezeRestoredState(ctx, service); err != nil {
		db.Close()
		return err
	}
	if err = db.Close(); err != nil {
		return err
	}
	manifest.Database, err = describeBackupFile(ctx, destination, "coordica.db")
	if err != nil {
		return err
	}
	if err = writeBackupManifest(destination, manifest); err != nil {
		return err
	}
	success = true
	return nil
}

func freezeRestoredState(ctx context.Context, s *Service) error {
	return s.DB.Update(ctx, func(tx *store.Tx) error {
		credentials, err := store.List[domain.Credential](tx, "credential")
		if err != nil {
			return err
		}
		for _, c := range credentials {
			if err = tx.Delete("credential", c.Hash); err != nil {
				return err
			}
		}
		pairings, err := store.List[domain.Pairing](tx, "pairing")
		if err != nil {
			return err
		}
		for _, p := range pairings {
			if err = tx.Delete("pairing", p.ID); err != nil {
				return err
			}
		}
		nodes, err := store.List[domain.Node](tx, "node")
		if err != nil {
			return err
		}
		for _, n := range nodes {
			n.Revoked = true
			n.Version++
			if err = tx.Put("node", n.ID, n); err != nil {
				return err
			}
		}
		p, err := getProject(tx)
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		p.Paused = true
		p.Version++
		runs, err := store.List[domain.Run](tx, "run")
		if err != nil {
			return err
		}
		for _, r := range runs {
			if !active(r) {
				continue
			}
			var task domain.Task
			if err = tx.Get("task", r.TaskID, &task); err != nil {
				return err
			}
			s.settle(&p, &task, &r, r.GrantSeconds, true)
			r.State = "lost"
			r.Quiescent = false
			r.LeaseUntil = s.Now().Add(-time.Second)
			r.Detail = "备份恢复：旧执行未确认停止，禁止恢复原租约"
			task.Generation++
			task.HoldReason = "offline"
			if err = tx.Put("run", r.ID, r); err != nil {
				return err
			}
			if err = tx.Put("task", task.ID, task); err != nil {
				return err
			}
		}
		candidates, err := store.List[domain.Candidate](tx, "candidate")
		if err != nil {
			return err
		}
		for _, c := range candidates {
			if c.State == "validating" {
				c.State = "reviewing"
				c.Review = nil
				c.Checks = nil
				c.Version++
				if err = tx.Put("candidate", c.ID, c); err != nil {
					return err
				}
			}
		}
		intents, err := store.List[domain.Intent](tx, "intent")
		if err != nil {
			return err
		}
		for _, in := range intents {
			if in.State == "dispatching" {
				in.State = "unknown"
				in.Detail = "备份恢复：先查询真实远端，不能重复派发"
				if err = tx.Put("intent", in.ID, in); err != nil {
					return err
				}
			}
		}
		if err = tx.Put("project", p.ID, p); err != nil {
			return err
		}
		return s.audit(tx, Actor{ID: "recovery", Role: "owner"}, "backup.restored", p.ID, "暂停项目、撤销旧凭据/节点，需重新引导用户并对账")
	})
}
