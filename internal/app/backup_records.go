package app

import (
	"context"
	"coordica/internal/domain"
	"coordica/internal/gitops"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
)

type backupRecords struct {
	Tasks              []domain.Task
	Artifacts          map[string]domain.Artifact
	CandidateArtifacts map[string]domain.Artifact
	Candidates         []domain.Candidate
	Runs               []domain.Run
	PlanningRuns       []domain.PlanningRun
	Supplements        []domain.TestSupplementPlan
	Intents            []domain.Intent
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
		if _, err = db.ExecContext(ctx, "PRAGMA journal_mode=DELETE; PRAGMA secure_delete=ON; DELETE FROM idempotency; "+
			"DELETE FROM records WHERE kind IN ('credential','pairing'); VACUUM;"); err != nil {
			return result, err
		}
	}
	rows, err := db.QueryContext(ctx, `SELECT kind,id,data FROM records WHERE kind IN
 ('artifact','candidate_artifact','candidate','run','intent','planning_run','task','test_supplement','test_supplement_digest')
 ORDER BY kind,id`)
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
		if err := appendBackupRecord(&result, kind, id, b); err != nil {
			return result, err
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
	for _, task := range records.Tasks {
		if task.Verification != nil {
			if err := add(task.Verification.Commit); err != nil {
				return nil, err
			}
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
	for _, plan := range records.Supplements {
		if err := add(plan.Commit); err != nil {
			return nil, err
		}
	}
	oids := make([]gitops.OID, 0, len(set))
	for oid := range set {
		oids = append(oids, oid)
	}
	sort.Slice(oids, func(i, j int) bool { return oids[i] < oids[j] })
	return oids, nil
}

func appendBackupRecord(result *backupRecords, kind, id string, b []byte) error {
	var err error
	switch kind {
	case "task":
		var task domain.Task
		if err = json.Unmarshal(b, &task); err != nil {
			return err
		}
		result.Tasks = append(result.Tasks, task)
	case "artifact", "candidate_artifact":
		var a domain.Artifact
		if err = json.Unmarshal(b, &a); err != nil {
			return err
		}
		if a.ID == "" {
			return fmt.Errorf("artifact record has no identifier")
		}
		if old, ok := result.Artifacts[a.ID]; ok && (old.SHA256 != a.SHA256 || old.Size != a.Size) {
			return fmt.Errorf("artifact records disagree")
		}
		result.Artifacts[a.ID] = a
		if kind == "candidate_artifact" {
			result.CandidateArtifacts[id] = a
		}
	case "candidate":
		var c domain.Candidate
		if err = json.Unmarshal(b, &c); err != nil {
			return err
		}
		result.Candidates = append(result.Candidates, c)
	case "run":
		var r domain.Run
		if err = json.Unmarshal(b, &r); err != nil {
			return err
		}
		result.Runs = append(result.Runs, r)
	case "planning_run":
		var r domain.PlanningRun
		if err = json.Unmarshal(b, &r); err != nil {
			return err
		}
		result.PlanningRuns = append(result.PlanningRuns, r)
	case "test_supplement", "test_supplement_digest":
		var plan domain.TestSupplementPlan
		if err = json.Unmarshal(b, &plan); err != nil {
			return err
		}
		result.Supplements = append(result.Supplements, plan)
	case "intent":
		var in domain.Intent
		if err = json.Unmarshal(b, &in); err != nil {
			return err
		}
		result.Intents = append(result.Intents, in)
	}
	return nil
}
