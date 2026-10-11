package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/store"
)

func bkFixture(t *testing.T) (*Service, *Integrator, gitops.OID, string) {
	t.Helper()
	ctx := context.Background()
	binary, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git unavailable")
	}
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "coordica.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := New(db, dir)
	ownerFile, err := s.Bootstrap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	token, err := os.ReadFile(ownerFile)
	if err != nil {
		t.Fatal(err)
	}
	g := gitops.New(binary)
	repo := filepath.Join(dir, "repository")
	if err = g.Init(ctx, repo, false); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(repo, "base.txt"), []byte("base\n"), 0600); err != nil {
		t.Fatal(err)
	}
	b, err := g.CommitAll(ctx, repo, "base", gitops.Identity{})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(repo, "task.txt"), []byte("head\n"), 0600); err != nil {
		t.Fatal(err)
	}
	h, err := g.CommitAll(ctx, repo, "head", gitops.Identity{})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := g.BuildCandidate(ctx, repo, b, h, filepath.Join(dir, "candidate"), gitops.Identity{})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Join(dir, "artifacts"), 0700); err != nil {
		t.Fatal(err)
	}
	material, err := g.CreateBundle(ctx, repo, b, h, filepath.Join(dir, "artifacts", "material"))
	if err != nil {
		t.Fatal(err)
	}
	now := s.Now()
	ar := domain.Artifact{ID: "material", RunID: "completed_implementation", SHA256: material.SHA256, Size: material.Bytes, CreatedAt: now}
	err = s.DB.Update(ctx, func(tx *store.Tx) error {
		values := []struct {
			kind, id string
			value    any
		}{
			{"project", "project", domain.Project{ID: "project", Version: 1, Name: "backup", RepositoryURL: "https://github.com/example/unused.git", TargetBranch: "main", TotalBudgetSeconds: 1000, UsedSeconds: 5, ReservedSeconds: 55, PolicyVersion: 1}},
			{"task", "live_task", domain.Task{ID: "live_task", Version: 1, State: "executing", Generation: 1, AuthorizationVersion: 1, BudgetSeconds: 200, UsedSeconds: 5, ReservedSeconds: 55, CurrentRunID: "live_run"}},
			{"task", "pending_task", domain.Task{ID: "pending_task", Version: 1, State: "awaiting_integration", CurrentCandidateID: "pending_candidate", BudgetSeconds: 200, Generation: 1}},
			{"node", "old_node", domain.Node{ID: "old_node", Version: 1, Capacity: 1}},
			{"run", "live_run", domain.Run{ID: "live_run", Version: 1, TaskID: "live_task", NodeID: "old_node", Kind: "implement", State: "running", Generation: 1, AuthorizationVersion: 1, Base: string(b), GrantSeconds: 60, ElapsedSeconds: 5, StartedAt: now.Add(-5 * time.Second), LeaseUntil: now.Add(time.Minute), Deadline: now.Add(time.Minute)}},
			{"candidate", "pending_candidate", domain.Candidate{ID: "pending_candidate", Version: 1, TaskID: "pending_task", Base: string(b), Head: string(h), Main: string(b), Commit: string(candidate.Commit), State: "landing", PolicyVersion: 1}},
			{"intent", "pending_intent", domain.Intent{ID: "pending_intent", CandidateID: "pending_candidate", Expected: string(b), Desired: string(candidate.Commit), State: "dispatching", CreatedAt: now}},
			{"artifact", ar.ID, ar}, {"candidate_artifact", "pending_candidate", ar},
			{"credential", Hash("old-node-secret"), domain.Credential{ID: "old_node", Hash: Hash("old-node-secret"), Role: "node", NodeID: "old_node"}},
			{"pairing", "old_pairing", domain.Pairing{ID: "old_pairing", CodeHash: Hash("old-pairing-secret"), ExpiresAt: now.Add(time.Hour)}},
		}
		for _, v := range values {
			if err := tx.Put(v.kind, v.id, v.value); err != nil {
				return err
			}
		}
		return tx.Remember("old-node", "register", "digest", map[string]string{"token": "old-node-secret"})
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, &Integrator{Service: s, Git: g}, candidate.Commit, strings.TrimSpace(string(token))
}

func TestBackupRestoreRetainsPendingCandidateAndFreezesOldAuthority(t *testing.T) {
	ctx := context.Background()
	s, w, c, oldOwner := bkFixture(t)
	location, err := w.Backup(ctx, Actor{ID: "owner", Role: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(location, "owner.token")); !os.IsNotExist(err) {
		t.Fatal("owner token copied", err)
	}
	if _, err = s.Authenticate(ctx, oldOwner); err != nil {
		t.Fatal("backup changed live authority", err)
	}
	info, err := os.Stat(location)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0700 {
		t.Fatal("unsafe backup mode", info.Mode())
	}
	var manifest backupManifest
	b, err := os.ReadFile(filepath.Join(location, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &manifest); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range manifest.Git {
		if item.OID == string(c) {
			found = true
		}
	}
	if !found {
		t.Fatal("pending C absent from manifest")
	}
	offline, err := sql.Open("sqlite", filepath.Join(location, "coordica.db"))
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err = offline.QueryRow("SELECT count(*) FROM records WHERE kind IN ('credential','pairing')").Scan(&count); err != nil || count != 0 {
		t.Fatalf("old authority in backup %d %v", count, err)
	}
	if err = offline.QueryRow("SELECT count(*) FROM idempotency").Scan(&count); err != nil || count != 0 {
		t.Fatalf("cached tokens in backup %d %v", count, err)
	}
	offline.Close()
	databaseBytes, err := os.ReadFile(filepath.Join(location, "coordica.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"old-node-secret", Hash("old-node-secret"), Hash("old-pairing-secret"), Hash(oldOwner)} {
		if bytes.Contains(databaseBytes, []byte(secret)) {
			t.Fatal("revoked secret survived in raw backup database bytes")
		}
	}
	dest := filepath.Join(t.TempDir(), "restored")
	if err = Restore(ctx, location, dest, w.Git); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dest, "coordica.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	restored := New(db, dest)
	project, err := restored.Project(ctx)
	if err != nil || !project.Paused || project.ReservedSeconds != 0 || project.UsedSeconds != 60 {
		t.Fatalf("restored budget/project %+v %v", project, err)
	}
	err = db.View(ctx, func(tx *store.Tx) error {
		var node domain.Node
		if err := tx.Get("node", "old_node", &node); err != nil {
			return err
		}
		if !node.Revoked {
			t.Error("old node not revoked")
		}
		var run domain.Run
		if err := tx.Get("run", "live_run", &run); err != nil {
			return err
		}
		if run.State != "lost" || run.Quiescent || !run.LeaseUntil.Before(restored.Now()) {
			t.Errorf("old run revived %+v", run)
		}
		var in domain.Intent
		if err := tx.Get("intent", "pending_intent", &in); err != nil {
			return err
		}
		if in.State != "unknown" {
			t.Errorf("intent automatically repeated %+v", in)
		}
		var candidate domain.Candidate
		if err := tx.Get("candidate", "pending_candidate", &candidate); err != nil {
			return err
		}
		if candidate.Commit != string(c) || candidate.State != "landing" {
			t.Errorf("pending C regenerated %+v", candidate)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = restored.Authenticate(ctx, oldOwner); err == nil {
		t.Fatal("old owner remained authorized")
	}
	if _, err = restored.Authenticate(ctx, "old-node-secret"); err == nil {
		t.Fatal("old node remained authorized")
	}
	newOwnerFile, err := restored.Bootstrap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	newToken, err := os.ReadFile(newOwnerFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(newToken)) == oldOwner {
		t.Fatal("owner token reused")
	}
	if _, err = restored.Authenticate(ctx, strings.TrimSpace(string(newToken))); err != nil {
		t.Fatal(err)
	}
	if _, err = w.Git.CreateSnapshotBundle(ctx, filepath.Join(dest, "repository"), c, filepath.Join(t.TempDir(), "retained.bundle")); err != nil {
		t.Fatal("fixed C objects lost", err)
	}
}

func TestBackupFailsClosedOnCorruptArtifact(t *testing.T) {
	s, w, _, _ := bkFixture(t)
	if err := os.WriteFile(filepath.Join(s.Dir, "artifacts", "material"), []byte("changed after upload"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Backup(context.Background(), Actor{ID: "owner", Role: "owner"}); err == nil {
		t.Fatal("corrupt artifact published a successful backup")
	}
	entries, err := os.ReadDir(filepath.Join(s.Dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("partial backup advertised: %v", entries)
	}
}

func TestRestoreRefusesExistingTargetAndCorruptBackup(t *testing.T) {
	_, w, _, _ := bkFixture(t)
	ctx := context.Background()
	location, err := w.Backup(ctx, Actor{ID: "owner", Role: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	existing := t.TempDir()
	marker := filepath.Join(existing, "keep.txt")
	if err = os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = RestoreBackup(ctx, location, existing); err == nil {
		t.Fatal("existing target overwritten")
	}
	if b, e := os.ReadFile(marker); e != nil || string(b) != "keep" {
		t.Fatal("existing data changed", e)
	}
	if err = os.WriteFile(filepath.Join(location, "artifacts", "material"), []byte("corrupt backup"), 0600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "fresh")
	if err = RestoreBackup(ctx, location, target); err == nil {
		t.Fatal("corrupt backup restored")
	}
	if _, e := os.Stat(target); !os.IsNotExist(e) {
		t.Fatal("failed restore left target", e)
	}
}

func TestBackupIncludesPhaseBRecordsAndRemovesCredentials(t *testing.T) {
	s, w, _, token := bkFixture(t)
	owner := Actor{ID: "owner", Role: "owner"}
	ctx := context.Background()
	if e := s.DB.Update(ctx, func(tx *store.Tx) error {
		settings := domain.DefaultAutomationSettings()
		settings.Enabled = true
		values := []struct {
			kind, id string
			value    any
		}{
			{"automation", "current", settings},
			{"planning_document", "current", domain.PlanningDocument{Version: 3, AuthorizationVersion: 3, Objective: "Backed up goal", AllowedRoots: []string{"src"}, BudgetSeconds: 500, Status: "planning"}},
			{"leader_message", "m", domain.LeaderMessage{ID: "m", DocumentVersion: 3, Role: "owner", Text: "Discuss the plan"}},
			{"planning_run", "p", domain.PlanningRun{ID: "p", Version: 1, DocumentVersion: 3, Generation: 1, State: "queued"}},
			{"decision", "d", domain.Decision{ID: "d", Version: 1, SourceKind: "planning_run", SourceID: "p", SourceVersion: 1, Kind: "architecture", Status: "open"}},
		}
		for _, v := range values {
			if e := tx.Put(v.kind, v.id, v.value); e != nil {
				return e
			}
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	backup, e := w.Backup(ctx, owner)
	if e != nil {
		t.Fatal(e)
	}
	db, e := store.Open(filepath.Join(backup, "coordica.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	copyService := New(db, backup)
	snap, e := copyService.Export(ctx, owner)
	if e != nil {
		t.Fatal(e)
	}
	if !snap.Automation.Enabled || snap.PlanningDocument == nil || snap.PlanningDocument.Objective != "Backed up goal" || len(snap.LeaderMessages) != 1 || len(snap.PlanningRuns) != 1 || len(snap.Decisions) != 1 {
		t.Fatal("Phase B records omitted from consistent backup")
	}
	if _, e := copyService.Authenticate(ctx, strings.TrimSpace(token)); e == nil {
		t.Fatal("backup retained owner credential")
	}
}

func planningReceiptBackup(t *testing.T) (*serviceFixture, domain.PlanningRun, string) {
	t.Helper()
	f, d, result := planningFixture(t, true)
	scheduleTest(t, f, d, "receipt-backup")
	w := &PlanningWorker{Service: f.s}
	r, _, _, e := w.reserve(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	output, _ := json.Marshal(result)
	receipt := PlanningOutcome{Generation: r.Generation, DocumentVersion: r.DocumentVersion, FinishedAt: f.now.Add(time.Second), TreeStopped: true, ElapsedSeconds: 2, Output: string(output)}
	b, _ := json.Marshal(receipt)
	if e = os.MkdirAll(filepath.Dir(w.receipt(r.ID)), 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(w.receipt(r.ID), b, 0600); e != nil {
		t.Fatal(e)
	}
	// Unreferenced receipts must never enter the closure.
	if e = os.WriteFile(w.receipt("bogus"), []byte(`{}`), 0600); e != nil {
		t.Fatal(e)
	}
	backup, e := f.s.Backup(context.Background(), f.owner)
	if e != nil {
		t.Fatal(e)
	}
	return f, r, backup
}
func TestBackupPlanningQuietReceiptReplaysDBRunningAfterRestore(t *testing.T) {
	f, r, backup := planningReceiptBackup(t)
	ctx := context.Background()
	dest := filepath.Join(t.TempDir(), "restored")
	if e := RestoreBackup(ctx, backup, dest); e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(dest, "planning-outcomes", r.ID+".json")
	info, e := os.Stat(file)
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("receipt not restored privately", e)
	}
	if _, e = os.Stat(filepath.Join(dest, "planning-outcomes", "bogus.json")); !os.IsNotExist(e) {
		t.Fatal("unreferenced receipt restored")
	}
	db, e := store.Open(filepath.Join(dest, "coordica.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	restored := New(db, dest)
	restored.Now = func() time.Time { return f.now.Add(time.Hour) }
	if e = (&PlanningWorker{Service: restored}).Recover(ctx); e != nil {
		t.Fatal(e)
	}
	paused, e := restored.Snapshot(ctx, f.owner)
	if e != nil {
		t.Fatal(e)
	}
	if paused.PlanningRuns[0].State != "pending_result" || len(paused.Tasks) != 1 {
		t.Fatal("restore pause discarded timely receipt or derived tasks")
	}
	if e = db.Update(ctx, func(tx *store.Tx) error {
		p, e := getProject(tx)
		if e != nil {
			return e
		}
		p.Paused = false
		return tx.Put("project", p.ID, p)
	}); e != nil {
		t.Fatal(e)
	}
	if e = (&PlanningWorker{Service: restored}).Recover(ctx); e != nil {
		t.Fatal(e)
	}
	snap, e := restored.Snapshot(ctx, f.owner)
	if e != nil {
		t.Fatal(e)
	}
	if snap.PlanningRuns[0].State != "succeeded" || !snap.PlanningRuns[0].TreeStopped || snap.PlanningRuns[0].ElapsedSeconds != 2 || len(snap.Tasks) != 2 || snap.PlanningDocument.PlanningReservedSeconds != 0 {
		t.Fatalf("authentic timely receipt did not replay: %+v", snap.PlanningRuns)
	}
}
func TestBackupPlanningReceiptCorruptMissingAndBogusFailClosed(t *testing.T) {
	for _, scenario := range []string{"corrupt", "missing", "bogus-source", "unreferenced"} {
		t.Run(scenario, func(t *testing.T) {
			_, r, backup := planningReceiptBackup(t)
			file := filepath.Join(backup, "planning-outcomes", r.ID+".json")
			if scenario == "missing" {
				if e := os.Remove(file); e != nil {
					t.Fatal(e)
				}
			} else {
				b := []byte(`{}`)
				if e := os.WriteFile(file, b, 0600); e != nil {
					t.Fatal(e)
				}
			}
			// Even a recomputed manifest must not accept receipts for other sources.
			if scenario == "bogus-source" || scenario == "unreferenced" {
				raw, e := os.ReadFile(filepath.Join(backup, "manifest.json"))
				if e != nil {
					t.Fatal(e)
				}
				var m backupManifest
				if e = json.Unmarshal(raw, &m); e != nil {
					t.Fatal(e)
				}
				if scenario == "unreferenced" {
					newFile := filepath.Join(backup, "planning-outcomes", "invented.json")
					if e = os.Rename(file, newFile); e != nil {
						t.Fatal(e)
					}
					m.PlanningReceipts[0].Path = "planning-outcomes/invented.json"
				}
				actual, e := describeBackupFile(context.Background(), backup, m.PlanningReceipts[0].Path)
				if e != nil {
					t.Fatal(e)
				}
				m.PlanningReceipts[0] = actual
				raw, _ = json.Marshal(m)
				if e = os.WriteFile(filepath.Join(backup, "manifest.json"), raw, 0600); e != nil {
					t.Fatal(e)
				}
			}
			dest := filepath.Join(t.TempDir(), "restored")
			if e := RestoreBackup(context.Background(), backup, dest); e == nil {
				t.Fatal("invalid receipt restored")
			}
			if _, e := os.Stat(dest); !os.IsNotExist(e) {
				t.Fatal("failed restore left runnable directory")
			}
		})
	}
}
