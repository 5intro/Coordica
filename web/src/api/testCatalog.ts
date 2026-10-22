import type { AcceptanceCoverage, AcceptancePlan, TestCase, TestCasePage, TestSupplementPlan } from '../api.generated';
import { ApiClient, ApiError } from './client';

type ObjectValue = Record<string, unknown>;
const object = (v: unknown): v is ObjectValue => !!v && typeof v === 'object' && !Array.isArray(v);
const text = (v: unknown): v is string => typeof v === 'string';
const integer = (v: unknown): v is number => Number.isSafeInteger(v) && Number(v) >= 0;
const strings = (v: unknown) => Array.isArray(v) && v.length <= 32 && v.every(text);
export const categories = {
  black_box: '黑盒', white_box: '白盒', smoke: '冒烟', regression: '回归', golden: '黄金旅程', e2e: '端到端', lifecycle: '长生命周期',
};
export const evidenceLabel = (state: string) => ({
  passed: '通过', test_gap: '待补充', missing_case: '未关联测试', manual_required: '待人工验收', stale: '版本已变化',
  unrun: '未运行', unimplemented: '待实现', unreported: '缺少条目报告', failed: '失败', skipped: '已跳过',
  suite_disabled: '套件已停用', platform_gap: '缺少其他平台证据', acceptance_outdated: '待定义验收',
}[state] ?? '状态未知');
export const gapActionLabel = (action: string) => ({
  write_test: '补写测试或条目报告', run_test: '执行测试', repair_test_or_code: '分析失败并修复',
  review_test_binding: '核对测试关联', owner_acceptance: '人工验收', define_acceptance: '完善验收条款',
}[action] ?? '核对缺口');

export function isTestCase(v: unknown): v is TestCase {
  return object(v) && ['id', 'name', 'purpose', 'suite_id', 'check_name', 'report_format', 'external_id'].every((k) => text(v[k]))
    && integer(v.version) && typeof v.enabled === 'boolean' && typeof v.implemented === 'boolean'
    && strings(v.categories) && strings(v.steps) && strings(v.assertions)
    && (v.coverage === null || Array.isArray(v.coverage) && v.coverage.length <= 32
      && v.coverage.every((ref) => object(ref) && text(ref.requirement_id) && integer(ref.revision)
        && integer(ref.plan_version) && text(ref.criterion_id)));
}

export function isAcceptancePlan(v: unknown): v is AcceptancePlan {
  return object(v) && text(v.requirement_id) && integer(v.revision) && integer(v.version)
    && (v.criteria === null || Array.isArray(v.criteria) && v.criteria.length <= 32 && v.criteria.every((c) =>
      object(c) && text(c.id) && text(c.title) && ['automated', 'manual'].includes(String(c.method))));
}

export function checked<T>(v: unknown, predicate: (value: unknown) => value is T): T {
  if (!predicate(v)) throw new ApiError('invalid_response', '测试数据不完整，请刷新后重试。');
  return v;
}

export function isCoverage(v: unknown): v is AcceptanceCoverage {
  return object(v) && object(v.requirement) && text(v.requirement.id) && text(v.requirement.title)
    && integer(v.requirement.version) && isAcceptancePlan(v.plan) && text(v.commit) && text(v.digest)
    && typeof v.current === 'boolean' && typeof v.verified === 'boolean'
    && Array.isArray(v.criteria) && v.criteria.every((item) => object(item) && object(item.criterion)
      && text(item.criterion.id) && text(item.criterion.title) && text(item.state) && Array.isArray(item.cases)
      && item.cases.every((e) => object(e) && object(e.case) && text(e.case.id) && text(e.case.name)
        && integer(e.case.version) && text(e.state) && text(e.run_id) && text(e.platform)))
    && Array.isArray(v.gaps) && v.gaps.every((g) => object(g) && text(g.criterion_id) && text(g.case_id) && text(g.reason) && text(g.action))
    && (!v.verified || v.current && v.criteria.length > 0 && v.gaps.length === 0 && v.criteria.every((item) =>
      object(item) && object(item.criterion) && item.criterion.method === 'automated' && item.state === 'passed'
      && Array.isArray(item.cases) && item.cases.length > 0 && item.cases.every((e) => object(e) && e.state === 'passed'
        && text(e.run_id) && e.run_id.length > 0)));
}

export function isSupplement(v: unknown): v is TestSupplementPlan {
  return object(v) && ['requirement_id', 'source_id', 'commit', 'digest'].every((key) => text(v[key]))
    && integer(v.source_revision) && integer(v.plan_version) && Array.isArray(v.gaps);
}

export async function testCasePage(client: ApiClient, query: string, signal: AbortSignal): Promise<TestCasePage> {
  const value = await client.read<unknown>(`/tests/cases?${query}`, signal);
  return checked(value, (v): v is TestCasePage => object(v) && Array.isArray(v.items) && v.items.length <= 50
    && v.items.every(isTestCase) && text(v.next));
}
