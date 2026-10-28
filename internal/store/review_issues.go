package store

import (
	"coordica/internal/domain"
	"encoding/json"
)

func ReviewIssues(tx *Tx, q domain.ReviewIssueQuery) (domain.ReviewIssuePage, error) {
	page := domain.ReviewIssuePage{Items: []domain.ReviewIssue{}}
	rows, err := tx.tx.QueryContext(tx.ctx, `SELECT data FROM records WHERE kind='review_issue' AND id>?
 AND json_extract(data,'$.task_id')=? ORDER BY id LIMIT ?`, q.After, q.TaskID, q.Limit+1)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var issue domain.ReviewIssue
		if err := rows.Scan(&raw); err != nil {
			return page, err
		}
		if err := json.Unmarshal(raw, &issue); err != nil {
			return page, err
		}
		page.Items = append(page.Items, issue)
	}
	if len(page.Items) > q.Limit {
		page.Items = page.Items[:q.Limit]
		page.Next = page.Items[len(page.Items)-1].ID
	}
	return page, rows.Err()
}
