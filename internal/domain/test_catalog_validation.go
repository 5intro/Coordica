package domain

import (
	"slices"
	"strings"
)

var TestCategories = []string{"black_box", "white_box", "smoke", "regression", "golden", "e2e", "lifecycle"}

func ValidateTestCase(v TestCase) error {
	if !ChatIdentifier.MatchString(v.ID) || !ChatText(v.Name, 512, true) || !ChatText(v.Purpose, 8192, true) ||
		len(v.Categories) == 0 || len(v.Categories) > len(TestCategories) || len(v.Coverage) > 32 ||
		!testInstructions(v.Steps) || !testInstructions(v.Assertions) || !ChatText(v.SuiteID, 200, false) ||
		!ChatText(v.CheckName, 128, false) || !ChatText(v.ExternalID, 1024, false) {
		return Fail("invalid_input", "测试条目需要名称、目的、分类、步骤和断言（每组 1..32 项）")
	}
	seen := map[string]bool{}
	for _, category := range v.Categories {
		if !slices.Contains(TestCategories, category) || seen[category] {
			return Fail("invalid_input", "测试分类无效或重复")
		}
		seen[category] = true
	}
	if v.ReportFormat != "" && v.ReportFormat != "go-json" {
		return Fail("invalid_input", "不支持此报告格式")
	}
	if v.Implemented && (!ChatIdentifier.MatchString(v.SuiteID) || !ChatText(v.CheckName, 128, true)) {
		return Fail("invalid_input", "已实现条目必须关联套件和检查命令")
	}
	if v.ReportFormat == "go-json" && (len(strings.Split(v.ExternalID, "#")) != 2 || !ChatText(v.ExternalID, 1024, true)) {
		return Fail("invalid_input", "Go 测试标识应为 package#TestName")
	}
	seen = map[string]bool{}
	for _, ref := range v.Coverage {
		key := ref.RequirementID + ":" + ref.CriterionID
		if !ChatIdentifier.MatchString(ref.RequirementID) || !ChatIdentifier.MatchString(ref.CriterionID) ||
			ref.Revision < 1 || ref.PlanVersion < 1 || seen[key] {
			return Fail("invalid_input", "验收关联必须唯一并绑定需求及验收版本")
		}
		seen[key] = true
	}
	return nil
}

func testInstructions(items []string) bool {
	if len(items) == 0 || len(items) > 32 {
		return false
	}
	for _, item := range items {
		if !ChatText(item, 2048, true) {
			return false
		}
	}
	return true
}

func ValidateAcceptancePlan(v AcceptancePlan) error {
	if !ChatIdentifier.MatchString(v.RequirementID) || v.Revision < 1 || len(v.Criteria) == 0 || len(v.Criteria) > 32 {
		return Fail("invalid_input", "验收计划需要需求版本和 1..32 个条款")
	}
	seen := map[string]bool{}
	for _, c := range v.Criteria {
		if !ChatIdentifier.MatchString(c.ID) || !ChatText(c.Title, 2048, true) || seen[c.ID] ||
			(c.Method != "automated" && c.Method != "manual") {
			return Fail("invalid_input", "验收条款需要唯一标识、说明和验证方式")
		}
		seen[c.ID] = true
	}
	return nil
}

func ValidateTestCaseQuery(v TestCaseQuery) error {
	if v.Limit < 1 || v.Limit > 50 || v.After != "" && !ChatIdentifier.MatchString(v.After) ||
		v.RequirementID != "" && !ChatIdentifier.MatchString(v.RequirementID) ||
		v.Category != "" && !slices.Contains(TestCategories, v.Category) {
		return Fail("invalid_input", "无效的测试列表条件，每页最多 50 项")
	}
	return nil
}
