package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"coordica/internal/domain"
	"coordica/internal/store"
)

// TestCatalog owns behavioral definitions, their versioned links, and queries.
// It cannot change commands, execute a process, accept a requirement or merge.
type TestCatalog struct{ Service *Service }

func (c TestCatalog) List(ctx context.Context, a Actor, q domain.TestCaseQuery) (domain.TestCasePage, error) {
	var page domain.TestCasePage
	if err := owner(a); err != nil {
		return page, err
	}
	if err := domain.ValidateTestCaseQuery(q); err != nil {
		return page, err
	}
	err := c.Service.DB.View(ctx, func(tx *store.Tx) error {
		var err error
		page, err = store.TestCases(tx, q)
		return err
	})
	return page, err
}

func (c TestCatalog) Save(ctx context.Context, a Actor, key string, in domain.TestCaseSave) (json.RawMessage, error) {
	if err := owner(a); err != nil {
		return nil, err
	}
	if err := domain.ValidateTestCase(in.Case); err != nil {
		return nil, err
	}
	return c.Service.mutate(ctx, a, key, "tests.case.save", in, func(tx *store.Tx) (any, error) {
		if _, err := getProject(tx); err != nil {
			return nil, err
		}
		var old domain.TestCase
		if err := tx.Get("test_case", in.Case.ID, &old); err != nil && !errors.Is(err, domain.ErrNotFound) {
			return nil, err
		}
		if err := version(old.Version, in.ExpectedVersion); err != nil {
			return nil, err
		}
		if err := validateCaseLinks(tx, in.Case); err != nil {
			return nil, err
		}
		v := in.Case
		v.Version = old.Version + 1
		if err := tx.Put("test_case", v.ID, v); err != nil {
			return nil, err
		}
		if err := tx.Put("test_case_revision", fmt.Sprintf("%s_v%d", v.ID, v.Version), v); err != nil {
			return nil, err
		}
		return v, c.Service.audit(tx, a, "tests.case.saved", v.ID, fmt.Sprint(v.Version))
	})
}

func validateCaseLinks(tx *store.Tx, v domain.TestCase) error {
	if v.Implemented {
		var suite domain.TestSuite
		if err := tx.Get("test_suite", v.SuiteID, &suite); err != nil {
			return err
		}
		if !slices.ContainsFunc(suite.Checks, func(check domain.Check) bool { return check.Name == v.CheckName }) {
			return domain.Fail("invalid_input", "检查命令不属于当前套件")
		}
	}
	for _, ref := range v.Coverage {
		var r domain.Requirement
		var plan domain.AcceptancePlan
		if err := tx.Get("requirement", ref.RequirementID, &r); err != nil {
			return err
		}
		if err := tx.Get("acceptance_plan", r.ID, &plan); err != nil {
			return err
		}
		if !domain.RequirementActive(r) || domain.RequirementRevisionNumber(r) != ref.Revision ||
			plan.Revision != ref.Revision || plan.Version != ref.PlanVersion {
			return domain.Fail("version_conflict", "需求或验收计划已更新，请重新核对关联")
		}
		if !slices.ContainsFunc(plan.Criteria, func(item domain.AcceptanceCriterion) bool { return item.ID == ref.CriterionID }) {
			return domain.Fail("invalid_input", "验收条款不存在")
		}
	}
	return nil
}
