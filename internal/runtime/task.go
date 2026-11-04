package runtime

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func (l *Ledger) SubmitTask(ctx context.Context, credential Credential, request SubmitTaskRequest) (TaskSnapshot, error) {
	if strings.TrimSpace(request.Title) == "" || len(request.Title) > 512 || !utf8.ValidString(request.Title) ||
		!content(request.Context) || request.ExternalRef != "" && !identifier(request.ExternalRef, 512) {
		return TaskSnapshot{}, ErrInvalid
	}
	id, err := freshID("task")
	if err != nil {
		return TaskSnapshot{}, err
	}
	return mutate(l, ctx, credential, Submit, request.Key, "task.submitted", request,
		func(*store.Tx, Grant) error { return nil },
		func(tx *store.Tx, grant Grant) (TaskSnapshot, string, bool, error) {
			now := time.Now().UTC()
			material := TaskContext{ID: compound(id, "1"), ApplicationID: grant.ApplicationID, TaskID: id,
				Version: 1, Content: request.Context, CreatedAt: now}
			task := Task{ID: id, ApplicationID: grant.ApplicationID, ExternalRef: request.ExternalRef,
				Title: request.Title, State: "open", Version: 1, ContextID: material.ID, ContextVersion: 1, CreatedAt: now}
			if err := putNew(tx, kindContext, material.ID, material); err != nil {
				return TaskSnapshot{}, "", false, err
			}
			if err := putNew(tx, kindTask, task.ID, task); err != nil {
				return TaskSnapshot{}, "", false, err
			}
			return TaskSnapshot{Task: task, Context: material}, task.ID, true, nil
		})
}

func (l *Ledger) ReadTask(ctx context.Context, credential Credential, id string) (TaskSnapshot, error) {
	var result TaskSnapshot
	if l == nil || l.db == nil || !identifier(id, 128) {
		return result, ErrInvalid
	}
	err := l.db.View(ctx, func(tx *store.Tx) error {
		grant, err := authorize(tx, credential, Read)
		if err != nil {
			return err
		}
		task, err := taskFor(tx, grant, id)
		if err == nil {
			result, err = taskSnapshot(tx, task)
		}
		return err
	})
	if err != nil {
		return TaskSnapshot{}, err
	}
	return result, nil
}

type inputBody struct {
	Kind           string `json:"kind"`
	TaskID         string `json:"task_id"`
	Content        string `json:"content"`
	RunID          string `json:"run_id,omitempty"`
	RunGeneration  int64  `json:"run_generation,omitempty"`
	ContextVersion int64  `json:"context_version,omitempty"`
}

func priorInput(tx *store.Tx, grant Grant, source Source, body inputBody) (Input, bool, error) {
	id := compound(grant.ApplicationID, source.ID, source.EventID, body.TaskID)
	var prior Input
	err := tx.Get(kindInput, id, &prior)
	if errors.Is(err, domain.ErrNotFound) {
		return Input{ID: id, ApplicationID: grant.ApplicationID, TaskID: body.TaskID, Source: source,
			Kind: body.Kind, Content: body.Content, Digest: digest(body), RunID: body.RunID,
			RunGeneration: body.RunGeneration, Disposition: "recorded"}, false, nil
	}
	if err == nil && (prior.ApplicationID != grant.ApplicationID || prior.TaskID != body.TaskID || prior.Digest != digest(body)) {
		err = ErrConflict
	}
	return prior, err == nil, err
}

// UpdateContext stores a complete bounded snapshot. It neither appends work to
// an existing Run nor changes any Run's pinned immutable context snapshot.
func (l *Ledger) UpdateContext(ctx context.Context, credential Credential, request ContextRequest) (Input, error) {
	if !identifier(request.TaskID, 128) || !sourceValid(request.Source) || !content(request.Content) ||
		request.ExpectedVersion <= 0 || request.ExpectedContextVersion <= 0 {
		return Input{}, ErrInvalid
	}
	var task Task
	return mutate(l, ctx, credential, WriteContext, request.Key, "context.updated", request,
		func(tx *store.Tx, grant Grant) (err error) {
			task, err = taskFor(tx, grant, request.TaskID)
			return err
		},
		func(tx *store.Tx, grant Grant) (Input, string, bool, error) {
			input, found, err := priorInput(tx, grant, request.Source, inputBody{Kind: "context", TaskID: task.ID, Content: request.Content})
			if err != nil || found {
				return input, input.ID, false, err
			}
			if err = writable(task, request.ExpectedVersion); err != nil {
				return Input{}, "", false, err
			}
			if task.ContextVersion != request.ExpectedContextVersion || task.ContextVersion == math.MaxInt64 {
				return Input{}, "", false, ErrConflict
			}
			task.Version++
			task.ContextVersion++
			task.ContextID = compound(task.ID, strconv.FormatInt(task.ContextVersion, 10))
			material := TaskContext{ID: task.ContextID, ApplicationID: task.ApplicationID, TaskID: task.ID,
				Version: task.ContextVersion, Content: request.Content, CreatedAt: time.Now().UTC()}
			input.ContextID, input.ContextVersion, input.TaskVersion = material.ID, material.Version, task.Version
			input.CreatedAt = material.CreatedAt
			if err = putNew(tx, kindContext, material.ID, material); err == nil {
				err = putNew(tx, kindInput, input.ID, input)
			}
			if err == nil {
				err = tx.Put(kindTask, task.ID, task)
			}
			return input, input.ID, true, err
		})
}

func (l *Ledger) ReadInput(ctx context.Context, credential Credential, id string) (Input, error) {
	var result Input
	if l == nil || l.db == nil || !identifier(id, 128) {
		return result, ErrInvalid
	}
	err := l.db.View(ctx, func(tx *store.Tx) error {
		grant, err := authorize(tx, credential, Read)
		if err != nil {
			return err
		}
		if err = tx.Get(kindInput, id, &result); err != nil {
			return err
		}
		if _, err = taskFor(tx, grant, result.TaskID); err != nil {
			return err
		}
		if result.ApplicationID != grant.ApplicationID {
			return ErrForbidden
		}
		if result.RunID != "" {
			_, err = runFor(tx, grant, result.TaskID, result.RunID, false)
		}
		return err
	})
	if err != nil {
		return Input{}, err
	}
	return result, nil
}
