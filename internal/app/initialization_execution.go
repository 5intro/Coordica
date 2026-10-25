package app

import (
	"context"
	"coordica/internal/codequality"
	"coordica/internal/domain"
	"coordica/internal/store"
	"encoding/json"
	"strings"
)

func executeInitializationLeader(callCtx context.Context, w *InitializationWorker,
	op domain.InitializationOperation, p domain.Project, v domain.Initialization, dir string,
) (PlanningOutcome, error) {
	s := w.Service
	for i := range p.Checks {
		if p.Checks[i].Args == nil {
			p.Checks[i].Args = []string{}
		}
	}
	input, _ := json.Marshal(struct {
		Initialization  domain.Initialization
		ActiveChecks    []domain.Check
		ActiveProtected []string
		Manifest        string
	}{v, p.Checks, p.ProtectedPaths, "source-manifest.json"})
	prompt := initializationInstruction + codequality.Instructions + string(input)
	if v.Kind == "new" {
		prompt = "COORDICA_INITIALIZATION_V1\nPUBLIC_RESEARCH_NEW_PROJECT\n" + strings.TrimPrefix(prompt, "COORDICA_INITIALIZATION_V1\n")
	}
	return executeObservedLeader(callCtx, s,
		domain.LeaderExecutionRef{Kind: "initialization", ID: op.ID, Generation: op.Generation, InputVersion: op.InitializationVersion},
		w.Executor, pinnedLeader(op.Leader), dir, prompt, func(pid int) error {
			return s.DB.Update(callCtx, func(tx *store.Tx) error {
				var current domain.InitializationOperation
				if e := tx.Get("initialization_operation", op.ID, &current); e != nil {
					return e
				}
				if initializationIssuer(tx, current) != nil || current.State != "running" || current.Generation != op.Generation || pid <= 0 {
					return domain.Fail("stale_run", "初始化启动已失效")
				}
				current.ProcessID = pid
				return tx.Put("initialization_operation", op.ID, current)
			})
		})
}
