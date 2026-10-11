import { emptyPhaseB } from '../test/phaseB';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { Project, Run, Snapshot, Task } from '../api.generated';
import { ApiClient } from '../api/client';
import { Tasks } from './Tasks';
import { Runs } from './Runs';
import { Settings } from './Settings';

const task: Task = { goal_version: 0, goal_authorization_version: 0,
  id: 'task-1', version: 7, title: '恢复测试', description: '目标', acceptance: '验收', requirement_ids: null,
  dependencies: null, allowed_paths: ['src/**'], hot_resources: null, tool: 'codex', state: 'ready',
  hold_reason: 'budget', approved_version: 2, authorization_version: 1, generation: 1,
  budget_seconds: 14400, used_seconds: 3600, reserved_seconds: 0, max_run_seconds: 3600,
  max_repairs: 2, max_retries: 2, repairs: 1, retries: 1, current_run_id: '', current_candidate_id: '', created_at: '',
};
const run: Run = { goal_version: 0, goal_authorization_version: 0,
  id: 'run-1', version: 3, task_id: task.id, node_id: 'node-1', kind: 'implement', tool: 'codex', state: 'lost',
  generation: 1, authorization_version: 1, policy_version: 1, lease_until: '', deadline: '', grant_seconds: 3600,
  elapsed_seconds: 3600, base: '', head: '', candidate_id: '', session_id: '', detail: '连接中断', quiescent: false, started_at: '',
};
const initial: Snapshot = { ...emptyPhaseB, api_version: 'v1', project: null, tasks: [task], requirements: null, runs: [run], candidates: null, nodes: null, events: null };

describe('explicit recovery actions', () => {
  it('requires a paused project and binds its budget approval before polling', async () => {
    const project: Project = { id: 'project', version: 3, name: 'Coordica', repository_url: '/tmp/repo', target_branch: 'main', paused: false, total_budget_seconds: 36000, used_seconds: 3600, reserved_seconds: 0, protected_paths: null, checks: null, policy_version: 1, created_at: '' };
    const client = new ApiClient('token');
    const write = vi.spyOn(client, 'write').mockResolvedValue(project);
    const view = render(<Settings project={project} client={client} refresh={() => undefined} />);
    expect((screen.getByRole('button', { name: '调整项目预算' }) as HTMLButtonElement).disabled).toBe(true);
    view.rerender(<Settings project={{ ...project, paused: true, version: 4 }} client={client} refresh={() => undefined} />);
    fireEvent.click(screen.getByRole('button', { name: '调整项目预算' }));
    fireEvent.change(screen.getByLabelText('项目累计预算（小时）'), { target: { value: '12' } });
    fireEvent.change(screen.getByLabelText('项目预算调整理由'), { target: { value: '增加有限迭代时间' } });
    view.rerender(<Settings project={{ ...project, paused: true, version: 5 }} client={client} refresh={() => undefined} />);
    fireEvent.click(screen.getByRole('button', { name: '批准项目预算' }));
    await waitFor(() => expect(write).toHaveBeenCalledWith('/project/budget', { expected_version: 4, budget_seconds: 43200, max_repairs: 0, max_retries: 0, reason: '增加有限迭代时间' }));
  });

  it('keeps the budget version originally reviewed when polling changes the task', async () => {
    const client = new ApiClient('token');
    const write = vi.spyOn(client, 'write').mockResolvedValue(task);
    const view = render(<Tasks snapshot={initial} client={client} refresh={() => undefined} />);
    fireEvent.click(screen.getByRole('button', { name: '调整预算' }));
    fireEvent.change(screen.getByLabelText('任务累计预算（小时）'), { target: { value: '5' } });
    fireEvent.change(screen.getByLabelText('预算调整理由'), { target: { value: '追加有限修复时间' } });
    view.rerender(<Tasks snapshot={{ ...initial, tasks: [{ ...task, version: 8, used_seconds: 4000 }] }} client={client} refresh={() => undefined} />);
    fireEvent.click(screen.getByRole('button', { name: '批准预算调整' }));
    await waitFor(() => expect(write).toHaveBeenCalledWith('/tasks/task-1/budget', { expected_version: 7, budget_seconds: 18000, max_repairs: 2, max_retries: 2, reason: '追加有限修复时间' }));
  });

  it('submits actual evidence against the originally displayed lost run version', async () => {
    const client = new ApiClient('token');
    const write = vi.spyOn(client, 'write').mockResolvedValue({ ...run, quiescent: true, state: 'stopped' });
    const view = render(<Runs snapshot={initial} client={client} refresh={() => undefined} />);
    fireEvent.click(screen.getByRole('button', { name: '确认已停止' }));
    fireEvent.change(screen.getByLabelText(/实际检查证据/), { target: { value: '已在执行电脑检查进程树，全部退出' } });
    view.rerender(<Runs snapshot={{ ...initial, runs: [{ ...run, version: 4, quiescent: true }] }} client={client} refresh={() => undefined} />);
    fireEvent.click(screen.getByRole('button', { name: '记录已停止证据' }));
    await waitFor(() => expect(write).toHaveBeenCalledWith('/runs/run-1/confirm-stopped', { expected_version: 3, evidence: '已在执行电脑检查进程树，全部退出' }));
  });
});
