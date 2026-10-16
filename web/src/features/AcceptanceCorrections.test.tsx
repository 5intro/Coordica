import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import type { Candidate, Snapshot } from '../api.generated';
import { ApiClient } from '../api/client';
import { centerSnapshot } from '../test/connectionInfo';
import { localRun, localTask, nodeSnapshot } from '../test/nodeSnapshot';
import { Overview } from './Overview';
import { Tasks } from './Tasks';
import { Requirements } from './Requirements';
import { Candidates } from './Candidates';
import { TaskForm } from './TaskForm';
import { PreparationRecovery } from './Recovery';
import { Agents } from './Agents';

const requirement = { id: 'req_verify', version: 1, title: '验证需求', body: '保留原文', created_at: '' };
const verified = { ...localTask, state: 'verified', requirement_ids: [requirement.id], current_run_id: '', hold_reason: '' };
const client = new ApiClient('owner');
const refresh = vi.fn();
const candidate: Candidate = { baseline_version: 1, baseline_digest: 'fixture', id: 'preparation', version: 4, task_id: localTask.id, source_run_id: localRun.id, authorization_version: 1, policy_version: 1, base: 'base', head: 'head', main: '', commit: '', state: 'rejected', detail: '', protected: false, approved_commit: '', checks: [], created_at: '' };
const stoppedRun = { ...localRun, state: 'succeeded', base: 'base', head: 'head', quiescent: true };
const recoverTask = { ...localTask, version: 9, state: 'awaiting_integration', current_run_id: '', current_candidate_id: candidate.id, hold_reason: 'human_decision' };
const recovery: Snapshot = { ...centerSnapshot, tasks: [recoverTask], runs: [stoppedRun], candidates: [{ ...candidate, preparation_stopped: true }] };

it('counts verified as completed, excludes cancellation from progress, and ends linked requirement activity', () => {
  const state = { ...centerSnapshot, tasks: [verified, { ...localTask, id: 'cancelled', state: 'cancelled' }], requirements: [requirement] };
  const view = render(<Overview snapshot={state} navigate={vi.fn()} />);
  expect(screen.getByText('1/1 已完成')).toBeTruthy();
  expect(within(screen.getByRole('region', { name: '任务池' })).queryByRole('button')).toBeNull();
  expect(within(screen.getByRole('region', { name: '需求池' })).queryByRole('button')).toBeNull();
  view.unmount();
  render(<Requirements requirements={[requirement]} tasks={[verified]} client={client} refresh={refresh} />);
  fireEvent.change(screen.getByLabelText('需求历史'), { target: { value: 'active' } });
  expect(screen.queryByRole('button', { name: requirement.title })).toBeNull();
  fireEvent.change(screen.getByLabelText('需求历史'), { target: { value: 'history' } });
  expect(screen.getByRole('button', { name: requirement.title })).toBeTruthy();
  expect(screen.getByText('验证完成')).toBeTruthy();
});
it('shows verified tasks in history without writes or pending assignment copy', () => {
  const write = vi.spyOn(client, 'write');
  render(<Tasks snapshot={{ ...centerSnapshot, tasks: [verified] }} client={client} refresh={refresh} />);
  for (const name of ['暂停', '取消任务', '调整预算', '调整范围', '恢复']) expect(screen.queryByRole('button', { name })).toBeNull();
  expect(screen.queryByText(/获批后由可用 Runner 领取/)).toBeNull();
  fireEvent.change(screen.getByLabelText('任务筛选'), { target: { value: 'active' } });
  expect(within(screen.getByRole('region', { name: '任务列表' })).queryByRole('button')).toBeNull();
  fireEvent.change(screen.getByLabelText('任务筛选'), { target: { value: 'history' } });
  expect(within(screen.getByRole('region', { name: '任务列表' })).getByRole('button', { name: verified.title })).toBeTruthy();
  expect(write).not.toHaveBeenCalled();
});
it('keeps precheck evidence accessible through a deep link while excluding it from the default review list', () => {
  const pending = { ...candidate, state: 'prechecking', commit: 'a'.repeat(40) };
  render(<Candidates snapshot={{ ...centerSnapshot, tasks: [localTask], candidates: [pending] }} initialId={pending.id} client={client} refresh={refresh} />);
  expect(within(screen.getByRole('region', { name: '候选列表' })).queryByRole('button')).toBeNull();
  expect(screen.getByText('预检查中 · 尚未进入 Agent 独立审查。')).toBeTruthy();
  fireEvent.change(screen.getByLabelText('审查筛选'), { target: { value: 'prechecking' } });
  expect(within(screen.getByRole('region', { name: '候选列表' })).getByRole('button')).toBeTruthy();
});
it('offers only actual failed verification Runs as ordinary task repair links and submits the real ID', async () => {
  const failed = { ...localRun, id: 'actual-failed-run', kind: 'verification', state: 'failed' };
  const write = vi.spyOn(client, 'write').mockResolvedValue({ id: 'repair-task' });
  const view = render(<TaskForm snapshot={{ ...centerSnapshot, runs: [failed, localRun, { ...failed, id: 'passed', state: 'succeeded' }] }} requirements={[]} tasks={[]} nodes={[]} client={client} refresh={refresh} onCreated={vi.fn()} onCancel={vi.fn()} />);
  const picker = screen.getByLabelText(/关联失败测试/);
  expect(within(picker).getAllByRole('option')).toHaveLength(2);
  fireEvent.change(picker, { target: { value: failed.id } });
  for (const [label, value] of [['任务标题', '修复失败'], ['目标与非目标', '普通代码修复'], ['可观察验收条件', '固定回归通过'], ['允许修改的路径', 'web/src/**'], ['实现工具', 'codex']]) fireEvent.change(screen.getByLabelText(new RegExp(`^${label}`)), { target: { value } });
  fireEvent.click(screen.getByRole('button', { name: '保存草稿' }));
  await waitFor(() => expect(write).toHaveBeenCalledWith('/tasks', expect.objectContaining({ test_run_id: failed.id, tool: 'codex' })));
  expect(write.mock.calls[0][1]).not.toHaveProperty('verification');
  expect(write).toHaveBeenCalledTimes(1);
  view.unmount();
  render(<Tasks snapshot={{ ...centerSnapshot, tasks: [{ ...localTask, test_run_id: failed.id }] }} client={client} refresh={refresh} />);
  expect(screen.getByRole('link', { name: '查看关联失败报告' }).getAttribute('href')).toBe('#testing?run=actual-failed-run');
});
it('requires an explicit preparation stop observation and a refreshed server fact before retry', async () => {
  const write = vi.spyOn(client, 'write').mockResolvedValue({});
  const props = { task: recoverTask, client, refresh };
  const state = { ...recovery, candidates: [{ ...candidate }] };
  const view = render(<PreparationRecovery {...props} snapshot={state} />);
  fireEvent.click(screen.getByText('高级恢复 · 候选准备失败'));
  expect(screen.getByRole('button', { name: '重试候选准备' })).toHaveProperty('disabled', true);
  fireEvent.change(screen.getByLabelText('准备进程停止的实际检查证据'), { target: { value: '在中心主机核验父进程和全部子进程均退出' } });
  // Polling cannot silently substitute a newer task version for this observation.
  view.rerender(<PreparationRecovery {...props} task={{ ...recoverTask, version: 10 }} snapshot={state} />);
  fireEvent.click(screen.getByRole('button', { name: '记录准备进程已停止观察' }));
  await screen.findByText('已记录准备进程停止观察；请刷新核对中心状态后再重试。');
  expect(write).toHaveBeenCalledWith('/tasks/task_local/confirm-preparation-stopped', { expected_version: 9, evidence: '在中心主机核验父进程和全部子进程均退出' });
  expect(screen.getByRole('button', { name: '重试候选准备' })).toHaveProperty('disabled', true);
  view.rerender(<PreparationRecovery {...props} task={{ ...recoverTask, version: 11 }} snapshot={recovery} />);
  fireEvent.click(screen.getByRole('button', { name: '重试候选准备' }));
  await waitFor(() => expect(write).toHaveBeenCalledWith('/tasks/task_local/retry-integration', { expected_version: 11 }));
});
it.each(['fixed-C', 'non-current', 'not-rejected', 'verification'])('never exposes preparation recovery for %s', (reason) => {
  const state = { ...recovery };
  let task = recoverTask;
  if (reason === 'fixed-C') state.candidates = [{ ...candidate, commit: 'a'.repeat(40) }];
  if (reason === 'non-current') task = { ...task, current_candidate_id: 'different' };
  if (reason === 'not-rejected') state.candidates = [{ ...candidate, state: 'reviewing' }];
  if (reason === 'verification') task = { ...task, state: 'verified' };
  render(<PreparationRecovery task={task} snapshot={state} client={client} refresh={refresh} />);
  expect(screen.queryByText('高级恢复 · 候选准备失败')).toBeNull();
});
it.each(['active-run', 'no-material', 'authority', 'policy', 'baseline', 'budget', 'retries', 'not-stopped', 'bad-handoff'])('disables retry on %s', (reason) => {
  const state: Snapshot = { ...recovery };
  let task = recoverTask;
  if (reason === 'active-run') state.runs = [{ ...stoppedRun, quiescent: false }];
  if (reason === 'no-material') state.runs = [];
  if (reason === 'authority') task = { ...task, authorization_version: 2 };
  if (reason === 'policy') state.project = { ...state.project!, policy_version: 2 };
  if (reason === 'baseline') task = { ...task, baseline_digest: 'changed' };
  if (reason === 'budget') task = { ...task, used_seconds: task.budget_seconds };
  if (reason === 'retries') task = { ...task, retries: task.max_retries };
  if (reason === 'not-stopped') state.candidates = [{ ...candidate, preparation_stopped: false }];
  if (reason === 'bad-handoff') state.runs = [{ ...stoppedRun, head: 'changed' }];
  const write = vi.spyOn(client, 'write');
  render(<PreparationRecovery task={task} snapshot={state} client={client} refresh={refresh} />);
  fireEvent.click(screen.getByText('高级恢复 · 候选准备失败'));
  fireEvent.click(screen.getByRole('button', { name: '重试候选准备' }));
  expect(screen.getByRole('button', { name: '重试候选准备' })).toHaveProperty('disabled', true);
  expect(write).not.toHaveBeenCalled();
});
it('surfaces the center rejection without inferring recovery permission from error text', async () => {
  vi.spyOn(client, 'write').mockRejectedValue(new Error('original stopped handoff required'));
  render(<PreparationRecovery task={recoverTask} snapshot={recovery} client={client} refresh={refresh} />);
  fireEvent.click(screen.getByText('高级恢复 · 候选准备失败'));
  fireEvent.click(screen.getByRole('button', { name: '重试候选准备' }));
  expect((await screen.findByRole('alert')).textContent).toBe('original stopped handoff required');
  expect(screen.queryByText('已请求有限准备重试，以中心返回状态为准。')).toBeNull();
});
it('groups six capabilities under one host with management controls once and separates center program execution', () => {
  const node = { ...nodeSnapshot.node, capabilities: Array.from({ length: 6 }, (_, i) => ({ ...nodeSnapshot.node.capabilities![0], tool: `tool-${i}`, detail: `诊断-${i}` })) };
  render(<Agents snapshot={{ ...centerSnapshot, nodes: [node], tasks: [localTask], runs: [{ ...localRun, node_id: 'center', kind: 'verification', tool: 'program' }] }} client={client} refresh={refresh} navigate={vi.fn()} />);
  expect(screen.getByRole('heading', { name: '中心程序执行' })).toBeTruthy();
  fireEvent.click(screen.getByText('主机设置'));
  expect(screen.getAllByRole('button', { name: '配置调度权限' })).toHaveLength(1);
  expect(screen.getAllByRole('button', { name: '撤销节点' })).toHaveLength(1);
  expect(screen.getByText('工具能力与技术详情').closest('details')).toHaveProperty('open', false);
  fireEvent.click(screen.getByText('工具能力与技术详情'));
  expect(within(screen.getByRole('table')).getAllByRole('row')).toHaveLength(7);
  fireEvent.change(screen.getByLabelText('搜索 Agent'), { target: { value: '诊断-4' } });
  expect(within(screen.getByRole('table')).getAllByRole('row')).toHaveLength(2);
  expect(within(screen.getByRole('table')).getByText('tool-4')).toBeTruthy();
});
it('removes an already open scope or budget editor when a verification task completes', () => {
  const working = { ...verified, state: 'executing' };
  const view = render(<Tasks snapshot={{ ...centerSnapshot, tasks: [working] }} client={client} refresh={refresh} />);
  fireEvent.click(screen.getByRole('button', { name: '调整范围' }));
  fireEvent.click(screen.getByRole('button', { name: '调整预算' }));
  expect(screen.getByRole('button', { name: '批准新范围' })).toBeTruthy();
  expect(screen.getByRole('button', { name: '批准预算调整' })).toBeTruthy();
  view.rerender(<Tasks snapshot={{ ...centerSnapshot, tasks: [verified] }} client={client} refresh={refresh} />);
  expect(screen.queryByRole('button', { name: '批准新范围' })).toBeNull();
  expect(screen.queryByRole('button', { name: '批准预算调整' })).toBeNull();
  expect(screen.queryByRole('region', { name: '范围请求历史' })).toBeNull();
});

it('groups task title and status without repeating technical identifiers or empty blocks in the list', () => {
  render(<Tasks snapshot={{ ...centerSnapshot, tasks: [{ ...verified, current_candidate_id: 'candidate-raw' }] }} client={client} refresh={refresh} />);
  const list = within(screen.getByRole('region', { name: '任务列表' }));
  expect(list.queryByText(verified.id)).toBeNull();
  expect(list.queryByText('candidate-raw')).toBeNull();
  expect(list.queryByText(/无.*阻断/)).toBeNull();
  expect(list.getByRole('link', { name: '查看当前候选 →' }).getAttribute('href')).toBe('#candidates?id=candidate-raw');
  expect(list.getByRole('button', { name: verified.title }).parentElement?.textContent).toContain('验证完成');
  expect(list.queryByText('技术详情')).toBeNull();
  expect(screen.getByText('candidate-raw').closest('details')?.open).toBe(false);
});

it('translates known task blocks while preserving the raw reason and unknown values', () => {
  const view = render(<Tasks snapshot={{ ...centerSnapshot, tasks: [{ ...verified, hold_reason: 'scope_requested' }] }} client={client} refresh={refresh} />);
  expect(within(screen.getByRole('region', { name: '任务列表' })).getByText('阻断原因：请求调整范围')).toBeTruthy();
  expect(screen.getByText('scope_requested').closest('details')?.open).toBe(false);
  expect(view.container.querySelector('[class*="eyebrow"]')).toBeNull();
  view.unmount();
  render(<Overview snapshot={{ ...centerSnapshot, tasks: [{ ...localTask, hold_reason: 'owner-custom-reason' }] }} navigate={vi.fn()} />);
  expect(within(screen.getByRole('region', { name: '任务池' })).getByText('阻断原因：owner-custom-reason')).toBeTruthy();
});
