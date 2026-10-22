package store

import "coordica/internal/domain"

// Project scalar metadata only: large pending plans and model output never
// enter the activity inventory. Each execution kind has a fixed small bound.
func LeaderExecutionHeads(tx *Tx, kind, id, clientID string) ([]domain.LeaderExecution, error) {
	recordKind, input := "planning_run", "document_version"
	if kind == "initialization" {
		recordKind, input = "initialization_operation", "initialization_version"
	}
	query := `SELECT id,COALESCE(json_extract(data,'$.generation'),0),COALESCE(json_extract(data,'$.` + input + `'),0),
 COALESCE(json_extract(data,'$.version'),0),COALESCE(json_extract(data,'$.purpose'),?),
 COALESCE(NULLIF(json_extract(data,'$.conversation_id'),''),NULLIF(json_extract(data,'$.requirement_change_id'),''),
 NULLIF(json_extract(data,'$.scope_request_id'),''),''),
 CASE WHEN COALESCE(json_extract(data,'$.conversation_id'),'')<>'' THEN 'conversation'
 WHEN COALESCE(json_extract(data,'$.requirement_change_id'),'')<>'' THEN 'requirement_change'
 WHEN COALESCE(json_extract(data,'$.scope_request_id'),'')<>'' THEN 'scope_request' ELSE '' END,
 COALESCE(json_extract(data,'$.state'),''),COALESCE(json_extract(data,'$.tree_stopped'),0),
 COALESCE(json_extract(data,'$.settled'),0) FROM records WHERE kind=?`
	purpose := ""
	if kind == "initialization" {
		purpose = "initialization"
	}
	args := []any{purpose, recordKind}
	if clientID != "" && kind == "planning" {
		query += ` AND (COALESCE(json_extract(data,'$.purpose'),'')<>? OR EXISTS(
 SELECT 1 FROM records conversation WHERE conversation.kind='conversation'
 AND conversation.id=json_extract(records.data,'$.conversation_id')
 AND json_extract(conversation.data,'$.source.client_id')=?))`
		args = append(args, domain.ConsultationPurpose, clientID)
	}
	if id != "" {
		query += " AND id=?"
		args = append(args, id)
	}
	query += ` ORDER BY CASE WHEN json_extract(data,'$.state') IN ('running','unknown_live','cancel_requested') THEN 0
 WHEN json_extract(data,'$.state')='queued' THEN 1 ELSE 2 END,rowid DESC LIMIT 20`
	rows, err := tx.tx.QueryContext(tx.ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	heads := []domain.LeaderExecution{}
	for rows.Next() {
		item := domain.LeaderExecution{}
		item.Ref.Kind = kind
		if err := rows.Scan(&item.Ref.ID, &item.Ref.Generation, &item.Ref.InputVersion, &item.Version,
			&item.Purpose, &item.SourceID, &item.SourceKind, &item.State, &item.TreeStopped, &item.Settled); err != nil {
			return nil, err
		}
		heads = append(heads, item)
	}
	return heads, rows.Err()
}
