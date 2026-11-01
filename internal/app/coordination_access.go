package app

import (
	"encoding/json"

	"coordica/internal/domain"
	"coordica/internal/store"
)

type workReader struct {
	tx      *store.Tx
	actor   Actor
	binding domain.Credential
	project domain.Project
	epoch   string
}

func newWorkReader(tx *store.Tx, a Actor) (workReader, error) {
	r := workReader{tx: tx, actor: a}
	if a.Role != "owner" {
		var err error
		r.binding, err = chatCredential(tx, a)
		if err != nil {
			return r, err
		}
	}
	var err error
	r.project, err = getProject(tx)
	if err == nil {
		r.epoch, err = store.CoordinationEpoch(tx)
	}
	return r, err
}

func (r workReader) authorFilter() string {
	if r.actor.Role == "owner" {
		return ""
	}
	return r.actor.ID
}

func (r workReader) nodeBound() bool { return r.binding.ChatScope == domain.ChatScopeNode }

func currentChatTask(t domain.Task, v domain.Run) bool {
	return t.CurrentRunID == v.ID && t.Generation == v.Generation && t.GoalVersion == v.GoalVersion &&
		t.GoalAuthorizationVersion == v.GoalAuthorizationVersion && t.AuthorizationVersion == v.AuthorizationVersion
}

func (r workReader) canReadTask(t domain.Task) bool {
	if !r.nodeBound() {
		return true
	}
	var v domain.Run
	return t.CurrentRunID != "" && r.tx.Get("run", t.CurrentRunID, &v) == nil && v.NodeID == r.binding.NodeID && currentChatTask(t, v)
}

func (r workReader) anchor(a domain.WorkAnchor, history bool) (domain.WorkFact, error) {
	f, err := r.fact(domain.WorkReference{Kind: a.Kind, ID: a.ID})
	if err == nil || !history || !r.nodeBound() || a.Kind != "task" {
		return f, err
	}
	if err != domain.ErrNotFound {
		return f, err
	}
	known, e := store.HasWorkHistory(r.tx, store.WorkRead{ProjectID: r.project.ID, AuthorID: r.actor.ID, Anchor: a})
	if e != nil {
		return f, e
	}
	if !known {
		return f, domain.ErrNotFound
	}
	return unavailableWorkFact(domain.WorkReference{Kind: a.Kind, ID: a.ID}), nil
}

func unavailableWorkFact(ref domain.WorkReference) domain.WorkFact {
	// No current title, version, assignment, status or existence is disclosed.
	return domain.WorkFact{Reference: ref, State: "unavailable", Stale: true, Questions: []domain.WorkQuestion{}, NextAction: "view_saved_history"}
}

func (r workReader) seal(f domain.WorkFact, evidence any) (domain.WorkFact, error) {
	f.Available = true
	if f.Questions == nil {
		f.Questions = []domain.WorkQuestion{}
	}
	data, err := json.Marshal(struct {
		Epoch    string
		Fact     domain.WorkFact
		Evidence any
	}{r.epoch, f, evidence})
	if err != nil {
		return f, err
	}
	f.Reference.Stamp = Hash(string(data))
	return f, nil
}

func workText(text string) (string, bool) {
	text = redacted(text)
	runes := []rune(text)
	if len(runes) > 512 {
		return string(runes[:512]), true
	}
	return text, false
}

func workDescription(f *domain.WorkFact, title, text string) {
	var clipped bool
	f.Title, clipped = workText(title)
	f.Text, f.Truncated = workText(text)
	f.Truncated = f.Truncated || clipped
}
