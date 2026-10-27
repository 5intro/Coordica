package app

import (
	"encoding/json"
	"path/filepath"

	"coordica/internal/domain"
	"coordica/internal/localfile"
	"coordica/internal/store"
)

// Owner receipts are context, never substitutes for current goal authorization.
type planningDecisionIndex struct {
	ID                   string `json:"id"`
	Version              int64  `json:"version"`
	SourceKind           string `json:"source_kind"`
	SourceID             string `json:"source_id"`
	SourceVersion        int64  `json:"source_version"`
	DocumentVersion      int64  `json:"document_version"`
	AuthorizationVersion int64  `json:"authorization_version"`
	Kind                 string `json:"kind"`
	Status               string `json:"status"`
}

func currentPlanningDecisions(tx *store.Tx, document domain.PlanningDocument) ([]domain.Decision, error) {
	all, err := store.List[domain.Decision](tx, "decision")
	if err != nil {
		return nil, err
	}
	current := []domain.Decision{}
	for _, decision := range all {
		if decision.DocumentVersion == document.Version && decision.AuthorizationVersion == document.AuthorizationVersion {
			current = append(current, decision)
		}
	}
	return current, nil
}

func appendPlanningDecisions(dir string, materials *leaderMaterials, decisions []domain.Decision) ([]planningDecisionIndex, error) {
	index := make([]planningDecisionIndex, 0, len(decisions))
	if len(decisions) == 0 {
		return index, nil
	}
	data, err := encodeLeaderRows(decisions)
	if err != nil {
		return nil, err
	}
	path := "context/decisions.jsonl"
	if err = localfile.WritePrivate(filepath.Join(dir, filepath.FromSlash(path)), data); err != nil {
		return nil, err
	}
	materials.Files = append(materials.Files, leaderMaterialFile{
		Path: path, SHA256: Hash(string(data)), Bytes: len(data), Records: len(decisions),
	})
	raw, err := json.Marshal(materials)
	if err != nil {
		return nil, err
	}
	if err = localfile.WritePrivate(filepath.Join(dir, "context", "manifest.json"), raw); err != nil {
		return nil, err
	}
	for _, decision := range decisions {
		index = append(index, planningDecisionIndex{ID: decision.ID, Version: decision.Version,
			SourceKind: decision.SourceKind, SourceID: decision.SourceID, SourceVersion: decision.SourceVersion,
			DocumentVersion: decision.DocumentVersion, AuthorizationVersion: decision.AuthorizationVersion,
			Kind: decision.Kind, Status: decision.Status})
	}
	return index, nil
}
