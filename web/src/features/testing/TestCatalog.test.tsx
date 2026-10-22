import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import type { AcceptanceCoverage, AcceptancePlan, TestCase } from '../../api.generated';
import { ApiClient, ApiError } from '../../api/client';
import { centerSnapshot } from '../../test/connectionInfo';
import { TestCases } from './TestCases';
import { RequirementAcceptance } from './RequirementAcceptance';
import { CoverageDetails } from './CoverageDetails';

const requirement = { id: 'req', version: 2, content_revision: 1, title: '一次支付', body: '仅支付一次', created_at: '', state: 'active' };
const plan: AcceptancePlan = { requirement_id: 'req', revision: 1, version: 1,
  criteria: [{ id: 'once', title: '重试不重复支付', method: 'automated' }] };
const snapshot = { ...centerSnapshot, requirements: [requirement] };
const testCase: TestCase = { id: 'pay_once', version: 1, name: '支付重试', purpose: '避免重复扣款', categories: ['black_box', 'regression'],
  steps: ['支付后重试'], assertions: ['只有一笔扣款'], enabled: true, implemented: false, suite_id: '', check_name: '',
  report_format: '', external_id: '', coverage: [] };
const coverage: AcceptanceCoverage = { requirement, plan, commit: 'a'.repeat(40), digest: 'b'.repeat(64), current: true,
  verified: false, criteria: [{ criterion: plan.criteria![0], state: 'missing_case', cases: [] }],
  gaps: [{ criterion_id: 'once', case_id: '', reason: 'missing_case', action: 'write_test' }] };

it('filters by category, pages bounded inventory and shows actual steps without raw identifiers', async () => {
  const client = new ApiClient('owner');
  const read = vi.spyOn(client, 'read').mockResolvedValue({ items: [testCase], next: 'pay_once' });
  render(<TestCases snapshot={snapshot} client={client} refresh={vi.fn()} />);
  fireEvent.click(await screen.findByRole('button', { name: /支付重试/ }));
  expect(screen.getByLabelText('操作步骤（每行一项）')).toHaveProperty('value', '支付后重试');
  expect(screen.queryByText('pay_once')).toBeNull();
  fireEvent.change(screen.getByLabelText('测试分类'), { target: { value: 'black_box' } });
  await waitFor(() => expect(read).toHaveBeenLastCalledWith('/tests/cases?limit=50&after=&category=black_box', expect.anything()));
  fireEvent.click(await screen.findByRole('button', { name: '下一批' }));
  await waitFor(() => expect(read).toHaveBeenLastCalledWith('/tests/cases?limit=50&after=pay_once&category=black_box', expect.anything()));
});

it('rejects incomplete catalog responses and permits refresh', async () => {
  const client = new ApiClient('owner');
  vi.spyOn(client, 'read').mockResolvedValueOnce({ items: [{ id: 'fake-green' }], next: '' })
    .mockResolvedValue({ items: [], next: '' });
  render(<TestCases snapshot={snapshot} client={client} refresh={vi.fn()} />);
  expect(await screen.findByRole('alert')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: '刷新条目' }));
  expect(await screen.findByText('此分类还没有测试条目。添加步骤与断言，明确要验证的行为。')).toBeTruthy();
});

it('does not replace a newer category with a late inventory response', async () => {
  const client = new ApiClient('owner');
  let resolve!: (v: unknown) => void;
  vi.spyOn(client, 'read').mockImplementationOnce(() => new Promise((done) => { resolve = done; }))
    .mockResolvedValue({ items: [], next: '' });
  render(<TestCases snapshot={snapshot} client={client} refresh={vi.fn()} />);
  fireEvent.change(screen.getByLabelText('测试分类'), { target: { value: 'white_box' } });
  await screen.findByText('此分类还没有测试条目。添加步骤与断言，明确要验证的行为。');
  await act(async () => resolve({ items: [testCase], next: '' }));
  expect(screen.queryByRole('button', { name: /支付重试/ })).toBeNull();
});

it('binds saved acceptance to the reviewed requirement and plan versions', async () => {
  const client = new ApiClient('owner');
  vi.spyOn(client, 'read').mockResolvedValue(plan);
  const write = vi.spyOn(client, 'write').mockResolvedValue({ ...plan, version: 2 });
  render(<RequirementAcceptance snapshot={snapshot} client={client} refresh={vi.fn()} />);
  fireEvent.change(screen.getByLabelText('关联需求'), { target: { value: 'req' } });
  await waitFor(() => expect(screen.getByRole('button', { name: '编辑验收条款' })).toHaveProperty('disabled', false));
  fireEvent.click(screen.getByRole('button', { name: '编辑验收条款' }));
  fireEvent.click(screen.getByRole('button', { name: '保存验收条款' }));
  await waitFor(() => expect(write).toHaveBeenCalledWith('/tests/acceptance', {
    expected_requirement_version: 2, expected_version: 1, plan,
  }));
});

it('generates supplementary work from the exact coverage digest and retains conflict feedback', async () => {
  const client = new ApiClient('owner');
  const write = vi.spyOn(client, 'write').mockRejectedValueOnce(new ApiError('version_conflict', '覆盖已变化', 409))
    .mockResolvedValue({ requirement_id: 'supplement', source_id: 'req', source_revision: 1, plan_version: 1,
      commit: coverage.commit, digest: coverage.digest, gaps: coverage.gaps });
  render(<CoverageDetails coverage={coverage} client={client} refresh={vi.fn()} />);
  fireEvent.click(screen.getByRole('button', { name: '生成补测需求' }));
  expect(await screen.findByText('覆盖已变化')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: '生成补测需求' }));
  expect((await screen.findByRole('link', { name: '查看补测需求' })).getAttribute('href')).toBe('#requirements?id=supplement');
  expect(write.mock.calls[1]).toEqual(write.mock.calls[0]);
  expect(write.mock.calls[0][1]).toEqual({ requirement_id: 'req', commit: coverage.commit, expected_digest: coverage.digest });
});

it('never offers automatic supplementary work from an outdated acceptance plan', () => {
  const client = new ApiClient('owner');
  render(<CoverageDetails coverage={{ ...coverage, current: false }} client={client} refresh={vi.fn()} />);
  expect(screen.getByRole('button', { name: '生成补测需求' })).toHaveProperty('disabled', true);
  expect(screen.queryByText('当前提交的自动测试证据齐备')).toBeNull();
});

it('manual-only gaps stay with the Owner instead of generating developer work', () => {
  const client = new ApiClient('owner');
  render(<CoverageDetails coverage={{ ...coverage, gaps: [{ criterion_id: 'once', case_id: '',
    reason: 'manual_required', action: 'owner_acceptance' }] }} client={client} refresh={vi.fn()} />);
  expect(screen.getByRole('button', { name: '生成补测需求' })).toHaveProperty('disabled', true);
});
