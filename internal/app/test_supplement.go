package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/store"
)

// Supplement proposes ordinary work in the existing requirement pipeline.
// It does not unpause the project, grant a goal, change commands or run tests.
func (c TestCatalog) Supplement(ctx context.Context, a Actor, key string, in domain.TestSupplementRequest) (json.RawMessage, error) {
	if err := owner(a); err != nil {
		return nil, err
	}
	commit, err := gitops.ParseOID(in.Commit)
	if err != nil || !domain.ChatIdentifier.MatchString(in.RequirementID) ||
		len(in.ExpectedDigest) != 64 {
		return nil, domain.Fail("invalid_input", "需要固定提交、需求及覆盖摘要")
	}
	return c.Service.mutate(ctx, a, key, "tests.supplement", in, func(tx *store.Tx) (any, error) {
		var previous domain.TestSupplementPlan
		if err := tx.Get("test_supplement_digest", in.ExpectedDigest, &previous); err == nil {
			if previous.SourceID != in.RequirementID || previous.Commit != string(commit) {
				return nil, domain.Fail("version_conflict", "补测摘要与请求不一致")
			}
			return previous, nil
		} else if !errors.Is(err, domain.ErrNotFound) {
			return nil, err
		}
		coverage, err := acceptanceCoverage(tx, in.RequirementID, string(commit))
		if err != nil {
			return nil, err
		}
		if !coverage.Current || coverage.Digest != in.ExpectedDigest {
			return nil, domain.Fail("version_conflict", "覆盖或需求已变化，请刷新补测预览")
		}
		if !slices.ContainsFunc(coverage.Gaps, func(g domain.TestGap) bool { return g.Action != "owner_acceptance" }) {
			return nil, domain.Fail("conflict", "没有自动补测缺口，人工验收请由 Owner 处理")
		}
		plan := domain.TestSupplementPlan{SourceID: in.RequirementID,
			SourceRevision: coverage.Plan.Revision, PlanVersion: coverage.Plan.Version,
			Commit: string(commit), Digest: coverage.Digest, Gaps: coverage.Gaps}
		title := []rune(coverage.Requirement.Title)
		if len(title) > 150 {
			title = title[:150]
		}
		body, err := supplementBody(tx, coverage)
		if err != nil {
			return nil, err
		}
		value, err := c.Service.createRequirement(tx, a, domain.Requirement{
			Title: "补测 · " + string(title), Body: body,
		})
		if err != nil {
			return nil, err
		}
		requirement := value.(domain.Requirement)
		plan.RequirementID = requirement.ID
		if err := linkSupplementRequirement(tx, coverage.Requirement, requirement); err != nil {
			return nil, err
		}
		if err := tx.Put("test_supplement", requirement.ID, plan); err != nil {
			return nil, err
		}
		if err := tx.Put("test_supplement_digest", coverage.Digest, plan); err != nil {
			return nil, err
		}
		return plan, c.Service.audit(tx, a, "tests.supplement.created", requirement.ID, coverage.Digest)
	})
}

func supplementBody(tx *store.Tx, c domain.AcceptanceCoverage) (string, error) {
	var b strings.Builder
	b.WriteString(supplementText(c))
	seen := map[string]bool{}
	for _, gap := range c.Gaps {
		if gap.CaseID == "" || seen[gap.CaseID] {
			continue
		}
		seen[gap.CaseID] = true
		var v domain.TestCase
		if err := tx.Get("test_case", gap.CaseID, &v); err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "\n测试条目 %s v%d：%s\n目的：%s\n步骤：%s\n断言：%s\n套件：%s；检查：%s；报告标识：%s\n",
			v.ID, v.Version, v.Name, v.Purpose, strings.Join(v.Steps, "；"), strings.Join(v.Assertions, "；"),
			v.SuiteID, v.CheckName, v.ExternalID)
		if b.Len() > 64<<10 {
			return "", domain.Fail("invalid_input", "补测计划材料超过 64 KiB，请拆分需求或精简测试条目")
		}
	}
	return b.String(), nil
}

func supplementText(c domain.AcceptanceCoverage) string {
	var b strings.Builder
	fmt.Fprintf(&b, "补测来源：%s，需求修订 %d，验收计划 v%d，固定提交 %s。\n",
		c.Requirement.ID, c.Plan.Revision, c.Plan.Version, c.Commit)
	b.WriteString("由 Leader 判断复用、补写或修复既有测试，沿原任务链路实施。不得放宽验收、修改授权或绕过 Owner 测试命令政策。\n")
	for _, item := range c.Criteria {
		fmt.Fprintf(&b, "验收条款 %s：%s（%s）\n", item.Criterion.ID, item.Criterion.Title, item.Criterion.Method)
	}
	for _, gap := range c.Gaps {
		fmt.Fprintf(&b, "缺口：条款 %s，测试 %s，验证 Run %s，原因 %s，建议 %s。\n",
			gap.CriterionID, gap.CaseID, gap.RunID, gap.Reason, gap.Action)
	}
	b.WriteString("人工验收项应保留给 Owner；只有实际结构化执行证据才算测试通过。完成代码仍须审查和固定提交的验证。")
	b.WriteString("失败验证的修复提案应使用上述 Run 作为 test_run_id，沿既有修复链路获取报告。")
	return b.String()
}

func linkSupplementRequirement(tx *store.Tx, source, next domain.Requirement) error {
	if len(source.RelatedIDs) >= 128 {
		return domain.Fail("invalid_input", "需求关联已达上限")
	}
	source.RelatedIDs = append(source.RelatedIDs, next.ID)
	next.RelatedIDs = []string{source.ID}
	source.Version++
	next.Version++
	if err := tx.Put("requirement", source.ID, source); err != nil {
		return err
	}
	return tx.Put("requirement", next.ID, next)
}
