import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Decision, PlanningRun, ScopeAssessment, Snapshot } from '../api.generated';
import { ApiClient } from '../api/client';
import { centerSnapshot } from '../test/connectionInfo';
import { localTask } from '../test/nodeSnapshot';
import { ScopeRequests } from './ScopeRequests';
import { Tasks } from './Tasks';
import { Planning } from './Planning';
import { Blocks } from './Blocks';
import { blockRecords } from './management';

const request: ScopeAssessment = { id: 'scope-1', version: 2, run_id: 'original-run', generation: 4, task_id: localTask.id, task_version: 8, authorization_version: 3, goal_version: 7, goal_authorization_version: 5, automation_version: 6, policy_version: 9, reason: 'Need the shared parser', paths: ['web/src/parser.ts', ' literal path '], state: 'queued' };
const planningRun: PlanningRun = { baseline_version: 1, baseline_digest: 'fixture', scope_request_id: request.id, id: 'scope-planning', version: 1, execution_adapter: 'codex_cli', process_id: 0, working_directory: '', task_mapping: [], document_version: 7, authorization_version: 5, automation_version: 6, generation: 1, state: 'lost', attempt: 1, max_retries: 2, grant_seconds: 30, elapsed_seconds: 12, lease_until: '', deadline: '', started_at: '', tree_stopped: false, settled: false, session_id: '', detail: '', task_ids: [] };
const snapshot: Snapshot = { ...centerSnapshot, tasks: [{ ...localTask, hold_reason: 'scope_requested' }], scope_requests: [request], planning_runs: [planningRun] };
const waitStopDecision: Decision = { id: 'wait-stop', version: 3, source_kind: 'scope_request', source_id: request.id, source_version: request.version, document_version: 7, authorization_version: 3, kind: 'scope_wait_stop', evidence: 'Original Run has not stopped', actions: ['inspect_run'], status: 'pending', resolution: '' };
const client = new ApiClient('private-owner-token');
afterEach(() => vi.unstubAllGlobals());

describe('read-only scope request facts', () => {
  it('shows queued requests without granting paths or changing task authorization', () => {
    const write = vi.spyOn(client, 'write');
    render(<Tasks snapshot={snapshot} client={client} refresh={vi.fn()} initialId={localTask.id} />);
    const history = screen.getByRole('region', { name: '范围请求历史' });
    expect(history.textContent).toContain('queued');
    expect(history.textContent).toContain('不代表范围已获授权');
    expect(within(history).getByText('literal path').textContent).toBe(' literal path ');
    expect(history.textContent).toContain('契约 v8 · 授权 v3');
    expect(history.textContent).toContain('目标 v7 · 目标授权 v5 · 自动化 v6 · 政策 v9');
    expect(history.textContent).toContain('来源 Run 代次 4');
    expect(screen.getByText('src/**')).toBeTruthy();
    expect(document.body.textContent).not.toContain('private-owner-token');
    expect(write).not.toHaveBeenCalled();
  });

  it('retains consumed history on Planning and links both actual source records', () => {
    render(<Planning snapshot={{ ...snapshot, scope_requests: [{ ...request, state: 'consumed' }] }} client={client} refresh={vi.fn()} />);
    const history = screen.getByRole('region', { name: '范围请求历史' });
    expect(history.textContent).toContain('consumed');
    expect(within(history).getByRole('link', { name: /原始 Run/ }).getAttribute('href')).toBe('#runs?id=original-run');
    expect(within(history).getByRole('link', { name: /关联 PlanningRun/ }).getAttribute('href')).toBe('#planning?id=scope-planning');
    expect(screen.getByText(/进程树未确认停止/)).toBeTruthy();
  });

  it('enriches one logical task block with stale request facts and source navigation', () => {
    const state = { ...snapshot, tasks: [{ ...localTask, version: 20, hold_reason: 'scope_requested' }], scope_requests: [{ ...request, state: 'blocked' }], blocks: [{ kind: 'task', object_id: localTask.id, reason: 'scope_requested', actions: ['inspect'] }, { kind: 'scope_request', object_id: request.id, reason: 'blocked', actions: [] }] };
    expect(blockRecords(state)).toHaveLength(1);
    const navigate = vi.fn();
    render(<Blocks snapshot={state} client={client} refresh={vi.fn()} navigate={navigate} />);
    const history = screen.getByRole('region', { name: '范围请求历史' });
    expect(history.textContent).toContain('阻断');
    expect(history.textContent).toContain('Need the shared parser');
    expect(history.textContent).toContain('契约 v8');
    fireEvent.click(within(history).getByRole('link', { name: /原始 Run/ }));
    expect(navigate).toHaveBeenCalledWith('runs', 'original-run');
    fireEvent.click(within(history).getByRole('link', { name: /关联 PlanningRun/ }));
    expect(navigate).toHaveBeenLastCalledWith('planning', 'scope-planning');
    expect(screen.getByRole('button', { name: /inspect/ })).toBeTruthy();
  });

  it('preserves pending requests and server-offered versioned decisions', async () => {
    const decision: Decision = { id: 'scope-decision', version: 3, source_kind: 'task', source_id: localTask.id, source_version: 8, document_version: 7, authorization_version: 3, kind: 'scope_requested', evidence: 'Server assessment', actions: ['cancel'], status: 'pending', resolution: '' };
    const state: Snapshot = { ...snapshot, scope_requests: [{ ...request, state: 'pending' }], decisions: [decision] };
    const write = vi.spyOn(client, 'write').mockResolvedValue({});
    expect(blockRecords(state)).toHaveLength(1);
    render(<Blocks snapshot={state} client={client} refresh={vi.fn()} navigate={vi.fn()} />);
    expect(screen.getByRole('region', { name: '范围请求历史' }).textContent).toContain('pending');
    const choices = screen.getByLabelText('决策操作');
    expect(within(choices).getAllByRole('option').map((option) => option.textContent)).toEqual(['选择操作', 'cancel']);
    fireEvent.change(choices, { target: { value: 'cancel' } });
    fireEvent.change(screen.getByLabelText('决策证据'), { target: { value: 'Owner reviewed the request' } });
    fireEvent.click(screen.getByRole('button', { name: '提交决策处理' }));
    await waitFor(() => expect(write).toHaveBeenCalledWith('/decisions/scope-decision/resolve', { expected_version: 3, expected_source_version: 8, action: 'cancel', evidence: 'Owner reviewed the request' }));
    expect(screen.getByRole('region', { name: '范围请求历史' }).textContent).toContain('pending');
  });

  it.each(['pending', 'resolved'])('makes %s inspect_run-only scope wait-stop decisions navigation-only', (status) => {
    const write = vi.spyOn(client, 'write');
    const navigate = vi.fn();
    const state: Snapshot = { ...snapshot, decisions: [{ ...waitStopDecision, status }], scope_requests: [{ ...request, id: 'another-request', run_id: 'another-run' }, request] };
    const { container } = render(<Blocks snapshot={state} client={client} refresh={vi.fn()} navigate={navigate} />);
    if (status === 'resolved') fireEvent.click(within(screen.getByRole('region', { name: '阻断列表' })).getByRole('button', { name: /已处理/ }));
    expect(container.querySelector('form')).toBeNull();
    expect(screen.queryByLabelText('决策操作')).toBeNull();
    expect(screen.queryByLabelText('决策证据')).toBeNull();
    expect(screen.queryByRole('button', { name: '提交决策处理' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: '核对原始 Run / 记录停止证明' }));
    expect(navigate).toHaveBeenCalledExactlyOnceWith('runs', request.run_id);
    expect(write).not.toHaveBeenCalled();
  });

  it.each([
    { label: 'null request data', requests: null },
    { label: 'missing exact source', requests: [{ ...request, id: 'another-request', run_id: 'another-run' }] },
    { label: 'missing original Run ID', requests: [{ ...request, run_id: '' }] },
  ])('reports unavailable original facts for $label without fallback navigation or writes', ({ requests }) => {
    const write = vi.spyOn(client, 'write');
    const navigate = vi.fn();
    const { container } = render(<Blocks snapshot={{ ...snapshot, scope_requests: requests, decisions: [waitStopDecision] }} client={client} refresh={vi.fn()} navigate={navigate} />);
    expect(screen.getByText(/原始 Run 来源事实不可用/).textContent).toContain(request.id);
    expect(screen.queryByRole('button', { name: '核对原始 Run / 记录停止证明' })).toBeNull();
    expect(screen.queryByRole('button', { name: /来源 v2/ })).toBeNull();
    expect(screen.queryByRole('button', { name: '提交决策处理' })).toBeNull();
    expect(container.querySelector('form')).toBeNull();
    expect(navigate).not.toHaveBeenCalled();
    expect(write).not.toHaveBeenCalled();
  });

  it('handles null and omitted additive data without creating records', async () => {
    const view = render(<ScopeRequests snapshot={{ ...centerSnapshot, scope_requests: null }} />);
    expect(screen.getByText('暂无范围请求记录。')).toBeTruthy();
    const { scope_requests: _requests, ...olderSnapshot } = centerSnapshot;
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify(olderSnapshot))));
    const older = await client.snapshot();
    view.rerender(<ScopeRequests snapshot={older} />);
    expect(screen.getByText('暂无范围请求记录。')).toBeTruthy();
    expect(screen.queryByRole('link')).toBeNull();
    expect(blockRecords(older)).toEqual([]);
  });
});
