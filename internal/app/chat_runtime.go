package app

import (
	"coordica/internal/domain"
	"coordica/internal/store"
)

func chatRuns(tx *store.Tx, binding domain.Credential) ([]domain.ChatRun, error) {
	runs, e := store.List[domain.Run](tx, "run")
	if e != nil {
		return nil, e
	}
	out := []domain.ChatRun{}
	for _, r := range runs {
		if binding.ChatScope == domain.ChatScopeNode && r.NodeID != binding.NodeID {
			continue
		}
		v := domain.ChatRun{ID: r.ID, Version: r.Version, State: r.State, Tool: redacted(r.Tool), Kind: r.Kind, NodeID: r.NodeID, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt, Deadline: r.Deadline, LeaseUntil: r.LeaseUntil, GrantSeconds: r.GrantSeconds, ElapsedSeconds: r.ElapsedSeconds, Quiet: r.Quiescent}
		v.ProgressVersion, v.ProgressCheckSeconds, v.Progress = r.ProgressVersion, r.ProgressCheckSeconds, r.Progress
		var t domain.Task
		// Historical ownership cannot reveal newer assignments or goal changes.
		if tx.Get("task", r.TaskID, &t) == nil && t.CurrentRunID == r.ID && t.Generation == r.Generation && t.GoalVersion == r.GoalVersion && t.GoalAuthorizationVersion == r.GoalAuthorizationVersion && t.AuthorizationVersion == r.AuthorizationVersion {
			v.TaskTitle = redacted(t.Title)
			v.TaskText = redacted(t.Description)
		}
		out = append(out, v)
	}
	return out, nil
}

// Use the same durable block aggregation as the Owner snapshot. Only its safe
// projection crosses the interaction boundary; no scheduler holds are invented.
func chatBlocks(tx *store.Tx, binding domain.Credential) ([]domain.ChatItem, error) {
	p, e := getProject(tx)
	if e != nil {
		return nil, e
	}
	v := domain.Snapshot{Project: &p}
	if v.Tasks, e = store.List[domain.Task](tx, "task"); e != nil {
		return nil, e
	}
	if v.Runs, e = store.List[domain.Run](tx, "run"); e != nil {
		return nil, e
	}
	if v.Candidates, e = store.List[domain.Candidate](tx, "candidate"); e != nil {
		return nil, e
	}
	if e = phaseBSnapshot(tx, &v); e != nil {
		return nil, e
	}
	out := []domain.ChatItem{}
	for _, b := range v.Blocks {
		if b.Kind == "run" && binding.ChatScope == domain.ChatScopeNode {
			own := false
			for _, r := range v.Runs {
				if r.ID == b.ObjectID && r.NodeID == binding.NodeID {
					own = true
				}
			}
			if !own {
				continue
			}
		}
		item := domain.ChatItem{ID: b.ObjectID, Kind: "blocks", Title: redacted(b.Kind), Status: redacted(b.Reason)}
		for _, t := range v.Tasks {
			if b.Kind == "task" && t.ID == b.ObjectID {
				item.Version = t.Version
				item.Title = redacted(t.Title)
				item.GoalVersion = t.GoalVersion
			}
		}
		for _, d := range v.Decisions {
			if b.Kind == "decision" && d.ID == b.ObjectID {
				item.Version = d.Version
				item.Title = redacted(d.Kind)
				item.Text = redacted(d.Evidence)
				item.GoalVersion = d.DocumentVersion
			}
		}
		for _, r := range v.PlanningRuns {
			if b.Kind == "planning_run" && r.ID == b.ObjectID {
				item.Version = r.Version
				item.GoalVersion = r.DocumentVersion
			}
		}
		out = append(out, item)
	}
	return out, nil
}
