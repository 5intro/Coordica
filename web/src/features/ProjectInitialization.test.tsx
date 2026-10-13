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

it('keeps blank draft analyzable and makes actual Owner checks a separate policy action', async () => {
  const client = new ApiClient('owner'); const write = vi.spyOn(client, 'write').mockResolvedValue(snapshot.project);
  render(<ProjectInitialization snapshot={{ ...snapshot, project: { ...snapshot.project!, checks: [] }, initialization: { ...readyInitialization, status: 'draft' } }} client={client} refresh={() => undefined} />);
  expect(screen.getByText(/当前尚无有效检查/)).toBeTruthy();
  expect((screen.getByRole('button', { name: '打开项目并请求只读初始化' }) as HTMLButtonElement).disabled).toBe(false);
  fireEvent.change(screen.getByLabelText(/实际检查 JSON/), { target: { value: JSON.stringify([{ name: 'Go tests', command: 'go', args: ['test', './...'], timeout_seconds: 600 }]) } });
  fireEvent.click(screen.getByRole('button', { name: '保存 Owner 检查政策' }));
  await waitFor(() => expect(write).toHaveBeenCalledWith('/project/policy', { expected_version: 1, checks: [{ name: 'Go tests', command: 'go', args: ['test', './...'], timeout_seconds: 600 }], protected_paths: ['internal/app/**'] }));
  expect(write).not.toHaveBeenCalledWith('/initialization/confirm', expect.anything());
});
