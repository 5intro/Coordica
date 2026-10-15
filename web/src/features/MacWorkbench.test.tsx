import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import type { Snapshot, TestSuite } from '../api.generated';
import { ApiClient } from '../api/client';
import { centerSnapshot } from '../test/connectionInfo';
import { localRun, localTask } from '../test/nodeSnapshot';
import { Testing } from './Testing';
import { Tasks } from './Tasks';
import { Settings } from './Settings';

const suite: TestSuite = { id: 'suite_internal', version: 2, name: '启动检查', description: '', stage: 'project', enabled: true, categories: [], platforms: [], match_paths: [], requirement_ids: [], checks: [] };
const commit = 'a'.repeat(40);
const client = new ApiClient('owner');
afterEach(() => { window.history.replaceState({}, '', '/'); vi.restoreAllMocks(); });

// Approximate the default text structure, without claiming browser layout validation.
function defaultText(container: HTMLElement) {
  const copy = container.cloneNode(true) as HTMLElement;
  copy.querySelectorAll('[hidden]').forEach((node) => node.remove());
  copy.querySelectorAll('details:not([open])').forEach((node) => {
    const summary = node.querySelector(':scope > summary')?.cloneNode(true);
    node.replaceChildren(...(summary ? [summary] : []));
  });
  return copy.textContent;
}

it('separates suite selection, contextual execution, and readiness into user-operated views', () => {
  const { container } = render(<Testing snapshot={{ ...centerSnapshot, test_suites: [suite] }} client={client} refresh={vi.fn()} initialCommit={commit} />);
  expect(screen.getByRole('button', { name: '套件' }).getAttribute('aria-pressed')).toBe('true');
  expect(screen.queryByRole('button', { name: '创建验证任务' })).toBeNull();
  expect(screen.queryByRole('button', { name: '查询就绪状态' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: '执行 启动检查' }));
  expect(screen.getByRole('button', { name: '创建验证任务' })).toBeTruthy();
  expect(defaultText(container)).not.toContain(suite.id);
  fireEvent.click(screen.getByRole('button', { name: '覆盖与就绪' }));
  expect(screen.queryByRole('button', { name: '创建验证任务' })).toBeNull();
  expect(screen.getByRole('button', { name: '查询就绪状态' })).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: '套件' }));
  expect(screen.getByLabelText('执行固定提交 SHA')).toHaveProperty('value', commit);
});

it('opens a report deep link in records and preserves complete bounded evidence', () => {
  const run = { ...localRun, id: 'run_private_evidence', kind: 'verification', test_report: { suite_id: suite.id, suite_version: 2, commit, digest: 'complete-digest', platform: 'darwin', checks: [{ name: '启动', passed: false, exit_code: 7, duration_seconds: 1, output: 'bounded failure evidence' }] } };
  window.history.replaceState({}, '', `/#testing?run=${run.id}`);
  render(<Testing snapshot={{ ...centerSnapshot, test_suites: [suite], runs: [run] }} client={client} refresh={vi.fn()} />);
  expect(screen.getByRole('button', { name: '执行记录' }).getAttribute('aria-pressed')).toBe('true');
  const report = screen.getByRole('article', { name: '测试报告 启动检查' });
  expect(report.querySelector(':scope > details')).toHaveProperty('open', true);
  expect(within(report).getByText(/退出码 7/)).toBeTruthy();
  const technical = within(report).getByText('技术详情').closest('details')!;
  expect(technical.open).toBe(false);
  fireEvent.click(within(technical).getByText('技术详情'));
  expect(technical.textContent).toContain(run.id);
  expect(technical.textContent).toContain(commit);
  expect(technical.textContent).toContain('complete-digest');
  expect(within(report).getByText('bounded failure evidence').closest('details')).toHaveProperty('open', false);
});

it('uses task names in the default list and retains original identifiers in technical details', () => {
  const task = { ...localTask, id: 'task_private_uid', title: '改进列表扫描', dependencies: [] };
  const state: Snapshot = { ...centerSnapshot, tasks: [task] };
  const { container } = render(<Tasks snapshot={state} client={client} refresh={vi.fn()} />);
  expect(screen.getByRole('button', { name: task.title })).toBeTruthy();
  expect(defaultText(container)).not.toContain(task.id);
  const technical = screen.getByText(task.id).closest('details')!;
  expect(technical.open).toBe(false);
  fireEvent.click(within(technical).getByText('技术详情'));
  expect(technical.textContent).toContain(task.id);
});

it('keeps grouped backup actions bound to the real API and shows success or failure', async () => {
  const write = vi.spyOn(client, 'write').mockResolvedValueOnce({ path: '/center/backup' }).mockRejectedValueOnce(new Error('backup denied'));
  render(<Settings project={centerSnapshot.project!} client={client} refresh={vi.fn()} />);
  const group = screen.getByRole('region', { name: '备份与导出' });
  fireEvent.click(within(group).getByRole('button', { name: '创建一致备份' }));
  await waitFor(() => expect(write).toHaveBeenCalledWith('/backups', {}));
  expect(await screen.findByText('/center/backup')).toBeTruthy();
  expect(screen.getByText('一致备份已完成。')).toBeTruthy();
  fireEvent.click(within(group).getByRole('button', { name: '创建一致备份' }));
  expect(await screen.findByRole('alert')).toHaveProperty('textContent', 'backup denied');
});
