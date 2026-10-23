import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { ApiClient } from '../api/client';
import type { Snapshot } from '../api.generated';
import { emptyPhaseB } from '../test/phaseB';
import { readyInitialization } from '../test/initialization';
import { ProjectInitialization } from './ProjectInitialization';

const snapshot: Snapshot = { ...emptyPhaseB, api_version: 'v1', initialization: readyInitialization,
  project: { id: 'project', version: 1, name: 'Project', repository_url: '/fixed/source', target_branch: 'main', paused: true, used_seconds: 0, reserved_seconds: 0, protected_paths: ['internal/app/**'], checks: readyInitialization.result!.baseline.checks, policy_version: 1, created_at: '' },
  requirements: [], tasks: [], runs: [], candidates: [], nodes: [], events: [] };
describe('project initialization', () => {
  it('requires exact reviewed version and digest, and invalidates review on refresh', async () => {
    const client = new ApiClient('owner'); const write = vi.spyOn(client, 'write').mockResolvedValue(readyInitialization);
    const view = render(<ProjectInitialization snapshot={snapshot} client={client} refresh={() => undefined} />);
    const confirm = screen.getByRole('button', { name: '确认已审阅的共享基线' }); expect((confirm as HTMLButtonElement).disabled).toBe(true);
    const checkbox = screen.getByRole('checkbox');
    for (const evidence of [screen.getByText('源码分析与覆盖记录'), screen.getByText('Owner 配置实际检查政策')]) {
      expect(evidence.compareDocumentPosition(checkbox) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    }
    fireEvent.click(screen.getByRole('checkbox')); expect((confirm as HTMLButtonElement).disabled).toBe(false); fireEvent.click(confirm);
    await waitFor(() => expect(write).toHaveBeenCalledWith('/initialization/confirm', { expected_version: 2, digest: 'b'.repeat(64) }));
    view.rerender(<ProjectInitialization snapshot={{ ...snapshot, initialization: { ...readyInitialization, version: 3, digest: 'd'.repeat(64) } }} client={client} refresh={() => undefined} />);
    expect((screen.getByRole('button', { name: '确认已审阅的共享基线' }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText('保留原始用户回答')).toBeTruthy();
  });
  it('cannot analyze while unpaused and sends only fixed-project selection', async () => {
    const client = new ApiClient('owner'); const write = vi.spyOn(client, 'write').mockResolvedValue(readyInitialization);
    const view = render(<ProjectInitialization snapshot={{ ...snapshot, project: { ...snapshot.project!, paused: false } }} client={client} refresh={() => undefined} />);
    expect((screen.getByRole('button', { name: '打开项目并请求只读初始化' }) as HTMLButtonElement).disabled).toBe(true);
    view.rerender(<ProjectInitialization snapshot={snapshot} client={client} refresh={() => undefined} />);
    fireEvent.click(screen.getByRole('button', { name: '打开项目并请求只读初始化' }));
    await waitFor(() => expect(write).toHaveBeenCalledWith('/initialization/analyze', { expected_version: 2, kind: 'existing' }));
  });
});

it('shows input selection before analyzing an unclassified new repository', async () => {
  const client = new ApiClient('owner');
  const write = vi.spyOn(client, 'write').mockResolvedValue(readyInitialization);
  render(<ProjectInitialization snapshot={{ ...snapshot, initialization: null }} client={client} refresh={() => undefined} />);
  const selector = screen.getByLabelText(/项目输入类型/);
  expect(selector.closest('details')).toHaveProperty('open', true);
  fireEvent.change(selector, { target: { value: 'new' } });
  fireEvent.click(screen.getByRole('button', { name: '打开项目并请求只读初始化' }));
  await waitFor(() => expect(write).toHaveBeenCalledWith('/initialization/analyze', { expected_version: 0, kind: 'new' }));
});

it('keeps blank draft analyzable and makes actual Owner checks a separate policy action', async () => {
  const client = new ApiClient('owner');
  const checks = [{ name: 'Go tests', command: 'go', args: ['test', './...'], timeout_seconds: 600 }];
  const write = vi.spyOn(client, 'write').mockResolvedValue({
    ...snapshot.project, version: 2, policy_version: 2, checks,
  });
  render(<ProjectInitialization snapshot={{ ...snapshot,
    project: { ...snapshot.project!, checks: [] },
    initialization: { ...readyInitialization, status: 'draft' },
  }} client={client} refresh={() => undefined} />);
  expect(screen.getByText(/当前尚无有效检查/)).toBeTruthy();
  expect(screen.getByRole('button', { name: '打开项目并请求只读初始化' })).toHaveProperty('disabled', false);
  fireEvent.change(screen.getByLabelText(/实际检查 JSON/), { target: { value: JSON.stringify(checks) } });
  fireEvent.click(screen.getByRole('button', { name: '保存 Owner 检查政策' }));
  await waitFor(() => expect(write).toHaveBeenCalledWith('/project/policy', {
    expected_version: 1, checks, protected_paths: ['internal/app/**'],
  }, expect.any(String)));
  await waitFor(() => expect(client.initializationPolicies.read('project')).toBeUndefined());
  expect(write).not.toHaveBeenCalledWith('/initialization/confirm', expect.anything());
});
