package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/store"
)

func (c TestCatalog) ReadAcceptance(ctx context.Context, a Actor, id string) (domain.AcceptancePlan, error) {
	plan := domain.AcceptancePlan{RequirementID: id, Criteria: []domain.AcceptanceCriterion{}}
	if err := owner(a); err != nil {
		return plan, err
	}
	if !domain.ChatIdentifier.MatchString(id) {
		return plan, domain.Fail("invalid_input", "需求标识无效")
	}
	err := c.Service.DB.View(ctx, func(tx *store.Tx) error {
		var r domain.Requirement
		if err := tx.Get("requirement", id, &r); err != nil {
			return err
		}
		plan.Revision = domain.RequirementRevisionNumber(r)
		err := tx.Get("acceptance_plan", id, &plan)
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return err
	})
	return plan, err
}

func (c TestCatalog) SaveAcceptance(ctx context.Context, a Actor, key string, in domain.AcceptanceSave) (json.RawMessage, error) {
	if err := owner(a); err != nil {
		return nil, err
	}
	if err := domain.ValidateAcceptancePlan(in.Plan); err != nil {
		return nil, err
	}
	return c.Service.mutate(ctx, a, key, "tests.acceptance.save", in, func(tx *store.Tx) (any, error) {
		var r domain.Requirement
		if err := tx.Get("requirement", in.Plan.RequirementID, &r); err != nil {
			return nil, err
		}
		if err := version(r.Version, in.ExpectedRequirementVersion); err != nil {
			return nil, err
		}
		if !domain.RequirementActive(r) || domain.RequirementRevisionNumber(r) != in.Plan.Revision {
			return nil, domain.Fail("version_conflict", "需求已修订或撤销")
		}
		var old domain.AcceptancePlan
		if err := tx.Get("acceptance_plan", r.ID, &old); err != nil && !errors.Is(err, domain.ErrNotFound) {
			return nil, err
		}
		if err := version(old.Version, in.ExpectedVersion); err != nil {
			return nil, err
		}
		v := in.Plan
		v.Version = old.Version + 1
		if err := tx.Put("acceptance_plan", r.ID, v); err != nil {
			return nil, err
		}
		if err := tx.Put("acceptance_revision", fmt.Sprintf("%s_v%d", r.ID, v.Version), v); err != nil {
			return nil, err
		}
		return v, c.Service.audit(tx, a, "tests.acceptance.saved", r.ID, fmt.Sprint(v.Version))
	})
}

func (c TestCatalog) Coverage(ctx context.Context, a Actor, id, commit string) (domain.AcceptanceCoverage, error) {
	var out domain.AcceptanceCoverage
	if err := owner(a); err != nil {
		return out, err
	}
	if !domain.ChatIdentifier.MatchString(id) {
		return out, domain.Fail("invalid_input", "需求标识无效")
	}
	oid, err := gitops.ParseOID(commit)
	if err != nil {
		return out, domain.Fail("invalid_input", "需要完整的固定提交 SHA")
	}
	err = c.Service.DB.View(ctx, func(tx *store.Tx) error {
		var err error
		out, err = acceptanceCoverage(tx, id, string(oid))
		return err
	})
	return out, err
}

func acceptanceCoverage(tx *store.Tx, id, commit string) (domain.AcceptanceCoverage, error) {
	out := domain.AcceptanceCoverage{Commit: commit, Criteria: []domain.CriterionCoverage{}, Gaps: []domain.TestGap{}}
	if err := tx.Get("requirement", id, &out.Requirement); err != nil {
		return out, err
	}
	if err := tx.Get("acceptance_plan", id, &out.Plan); err != nil && !errors.Is(err, domain.ErrNotFound) {
		return out, err
	}
	out.Current = domain.RequirementActive(out.Requirement) &&
		out.Plan.Revision == domain.RequirementRevisionNumber(out.Requirement) && out.Plan.Version > 0
	cases, err := coverageCases(tx, id)
	if err != nil {
		return out, err
	}
	out.Verified = out.Current && len(out.Plan.Criteria) > 0
	evidence := map[string]domain.CaseEvidence{}
	for _, criterion := range out.Plan.Criteria {
		item, err := criterionCoverage(tx, out.Plan, criterion, cases, commit, out.Current, evidence)
		if err != nil {
			return out, err
		}
		out.Criteria = append(out.Criteria, item)
		out.Gaps = append(out.Gaps, criterionGaps(item)...)
		out.Verified = out.Verified && item.State == "passed"
	}
	if !out.Current {
		out.Gaps = append(out.Gaps, domain.TestGap{Reason: "acceptance_outdated", Action: "define_acceptance"})
	}
	// Include every definition, even disabled/outdated links: edits invalidate a
	// preview instead of silently generating a plan from a different inventory.
	basis := out
	basis.Requirement = domain.Requirement{ID: out.Requirement.ID, ContentRevision: domain.RequirementRevisionNumber(out.Requirement),
		State: out.Requirement.State}
	bytes, _ := json.Marshal(struct {
		Coverage domain.AcceptanceCoverage
		Cases    []domain.TestCase
	}{basis, cases})
	out.Digest = Hash(string(bytes))
	return out, nil
}

func coverageCases(tx *store.Tx, id string) ([]domain.TestCase, error) {
	items := []domain.TestCase{}
	q := domain.TestCaseQuery{RequirementID: id, Limit: 50}
	for {
		page, err := store.TestCases(tx, q)
		if err != nil {
			return nil, err
		}
		items = append(items, page.Items...)
		if len(items) > 256 {
			return nil, domain.Fail("invalid_input", "单需求最多 256 个测试条目，请拆分需求")
		}
		if page.Next == "" {
			return items, nil
		}
		q.After = page.Next
	}
}
