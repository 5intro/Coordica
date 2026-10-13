import { fireEvent, render, screen, within, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { ApiClient } from '../api/client';
import { centerSnapshot } from '../test/connectionInfo';
import { localTask, localRun, nodeSnapshot } from '../test/nodeSnapshot';
import { Tasks } from './Tasks';
import { Requirements } from './Requirements';
import { Blocks } from './Blocks';
import { Agents } from './Agents';
import { Runs } from './Runs';
import { blockRecords } from './management';
import type { Candidate, Snapshot } from '../api.generated';

const client = new ApiClient('test-owner');
const refresh = vi.fn();
const candidate: Candidate = { baseline_version: 1, baseline_digest: 'fixture', id: 'candidate-fixed', version: 2, task_id: localTask.id, source_run_id: localRun.id, authorization_version: 1, policy_version: 1, base: 'B', head: 'H', main: 'M', commit: 'fixed-C', state: 'ready_to_land', detail: 'protected path', protected: true, approved_commit: '', checks: [], created_at: '' };
const snapshot: Snapshot = { ...centerSnapshot, tasks: [{ ...localTask, state: 'awaiting_integration', current_candidate_id: candidate.id, hold_reason: '' }, { ...localTask, id: 'held-task', hold_reason: 'budget' }], nodes: [nodeSnapshot.node], runs: [{ ...localRun, state: 'lost' }], candidates: [candidate] };

describe('management records', () => {
  it('combines task search, state, tool and hold filters and keeps dependencies selectable', () => {
    render(<Tasks snapshot={{ ...snapshot, tasks: [{ ...snapshot.tasks![0], hold_reason: 'budget', dependencies: ['dep'] }, { ...localTask, id: 'dep', title: '前置验收', tool: 'new-tool', state: 'integrated', current_run_id: '', hold_reason: '' }] }} client={client} refresh={refresh} />);
    const list = within(screen.getByRole('region', { name: '任务列表' }));
    fireEvent.change(screen.getByLabelText('搜索任务'), { target: { value: 'Windows-dsh budget 前置验收' } });
    fireEvent.change(screen.getByLabelText('任务工具'), { target: { value: 'dsh' } });
    fireEvent.change(screen.getByLabelText('任务阻塞'), { target: { value: 'held' } });
    expect(list.getByRole('button', { name: localTask.title })).toBeTruthy();
    fireEvent.click(list.getByRole('button', { name: '前置验收 →' }));
    expect(screen.getByRole('heading', { name: '前置验收' })).toBeTruthy();
    fireEvent.change(screen.getByLabelText('任务筛选'), { target: { value: 'integrated' } });
    expect(list.getByText('没有匹配任务')).toBeTruthy();
  });

  it('filters requirement sources, counts actual related tasks and navigates to details', () => {
    const navigate = vi.fn();
    render(<Requirements requirements={[{ id: 'req-node', version: 1, title: '节点需求', body: '搜索验收', source_node_id: 'node_local', created_at: '' }, { id: 'req-center', version: 1, title: '中心需求', body: '其他', created_at: '' }]} tasks={[{ ...localTask, requirement_ids: ['req-node'], state: 'integrated' }]} client={client} refresh={refresh} navigate={navigate} />);
    fireEvent.change(screen.getByLabelText('需求来源'), { target: { value: 'node_local' } });
    fireEvent.change(screen.getByLabelText('搜索需求'), { target: { value: '搜索 node_local' } });
    expect(screen.queryByRole('button', { name: '中心需求' })).toBeNull();
    expect(screen.getByText('1 / 1')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: `${localTask.title} →` }));
    expect(navigate).toHaveBeenCalledWith('tasks', localTask.id);
    fireEvent.change(screen.getByLabelText('搜索需求'), { target: { value: 'missing' } });
    expect(screen.getByText('没有匹配需求')).toBeTruthy();
  });

  it('keeps hold sources separate and excludes quiet lost runs, approved candidates and inferred waits', () => {
    const navigate = vi.fn();
    expect(blockRecords(snapshot).map((row) => row.id)).toEqual(['task:held-task', `run:${localRun.id}`, 'candidate:candidate-fixed']);
    expect(blockRecords({ ...snapshot, tasks: [localTask], runs: [{ ...localRun, state: 'lost', quiescent: true }], candidates: [{ ...candidate, approved_commit: candidate.commit }] })).toEqual([]);
    render(<Blocks client={client} refresh={refresh} snapshot={snapshot} navigate={navigate} />);
    fireEvent.change(screen.getByLabelText('阻塞来源'), { target: { value: '失联 Run' } });
    expect(screen.getByText('失联 · 未确认静止')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /核对进程/ }));
    expect(navigate).toHaveBeenCalledWith('runs', localRun.id);
    fireEvent.change(screen.getByLabelText('阻塞来源'), { target: { value: '固定候选' } });
    fireEvent.click(screen.getByRole('button', { name: /特别批准此 C/ }));
    expect(navigate).toHaveBeenCalledWith('candidates', candidate.id);
    fireEvent.change(screen.getByLabelText('搜索阻塞'), { target: { value: 'missing' } });
    expect(screen.getByText('没有匹配阻塞')).toBeTruthy();
  });

  it('uses dynamic tools and preserves lost work on revoked nodes with combined filters', () => {
    const navigate = vi.fn();
    render(<Agents snapshot={{ ...snapshot, nodes: [{ ...nodeSnapshot.node, revoked: true, capabilities: [{ ...nodeSnapshot.node.capabilities![0], tool: 'future-tool', available: false }] }, { ...nodeSnapshot.node, id: 'empty', name: '未报告节点', capabilities: [] }], runs: [{ ...localRun, tool: 'future-tool', state: 'lost' }] }} client={client} refresh={refresh} navigate={navigate} />);
    expect(screen.getByText('未报告工具')).toBeTruthy();
    fireEvent.change(screen.getByLabelText('Agent 工具'), { target: { value: 'future-tool' } });
    fireEvent.change(screen.getByLabelText('Agent 可用性'), { target: { value: 'revoked' } });
    fireEvent.change(screen.getByLabelText('Agent 节点'), { target: { value: 'node_local' } });
    expect(screen.getByText('已撤销', { selector: 'span' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: '撤销节点' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: /失联 · 未确认静止/ }));
    expect(navigate).toHaveBeenCalledWith('runs', localRun.id);
    expect(screen.getByText(/节点活动 1 \/ 容量 1/)).toBeTruthy();
    expect(screen.getByText('执行认证未提供')).toBeTruthy();
    fireEvent.change(screen.getByLabelText('Agent 可用性'), { target: { value: 'available' } });
    expect(screen.getByText('没有匹配 Agent')).toBeTruthy();
  });

  it('reuses node revocation and reports the center recovery outcome after stop evidence', async () => {
    const write = vi.spyOn(client, 'write').mockResolvedValue({});
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    const view = render(<Agents snapshot={snapshot} client={client} refresh={refresh} navigate={vi.fn()} />);
    fireEvent.click(screen.getByRole('button', { name: '撤销节点' }));
    await waitFor(() => expect(write).toHaveBeenCalledWith('/nodes/node_local/revoke', {}));
    view.unmount();
    render(<Runs snapshot={snapshot} client={client} refresh={refresh} />);
    fireEvent.click(screen.getByRole('button', { name: '确认已停止' }));
    fireEvent.change(screen.getByLabelText(/实际检查证据/), { target: { value: '进程树已退出' } });
    fireEvent.click(screen.getByRole('button', { name: '记录已停止证据' }));
    await screen.findByText('已记录停止证据，恢复结果以中心状态为准。');
    expect(write).toHaveBeenCalledWith('/runs/run_local/confirm-stopped', { expected_version: 1, evidence: '进程树已退出' });
    expect(screen.queryByText(/请在任务中显式恢复/)).toBeNull();
  });

  it('preserves requirement drafts on write failure and creates through the existing endpoint', async () => {
    const write = vi.spyOn(client, 'write').mockRejectedValueOnce(new Error('写入失败')).mockResolvedValueOnce({});
    render(<Requirements requirements={[]} tasks={[]} client={client} refresh={refresh} navigate={vi.fn()} />);
    expect(screen.getByText('需求池为空')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '记录需求 ＋' }));
    const title = screen.getByLabelText('需求标题') as HTMLInputElement;
    fireEvent.change(title, { target: { value: '真实需求' } });
    fireEvent.change(screen.getByLabelText('原始需求与成功条件'), { target: { value: '真实验收' } });
    fireEvent.click(screen.getByRole('button', { name: '保存需求' }));
    await screen.findByRole('alert');
    expect(title.value).toBe('真实需求');
    fireEvent.click(screen.getByRole('button', { name: '保存需求' }));
    await screen.findByText('需求已记录。');
    expect(write).toHaveBeenLastCalledWith('/requirements', { title: '真实需求', body: '真实验收' });
  });

  it('pairs from Agents using the shared existing form and handles server errors', async () => {
    const write = vi.spyOn(client, 'write').mockRejectedValueOnce(new Error('配对失败')).mockResolvedValueOnce({ code: 'one-time-code', expires_at: '2026-10-11T05:00:00Z' });
    render(<Agents snapshot={centerSnapshot} client={client} refresh={refresh} navigate={vi.fn()} />);
    fireEvent.click(screen.getByRole('button', { name: '配对节点 ＋' }));
    const name = screen.getByLabelText('节点名称') as HTMLInputElement;
    fireEvent.change(name, { target: { value: '测试电脑' } });
    fireEvent.click(screen.getByRole('button', { name: '生成配对码' }));
    await screen.findByText('配对失败');
    expect(name.value).toBe('测试电脑');
    fireEvent.click(screen.getByRole('button', { name: '生成配对码' }));
    await screen.findByText('one-time-code');
    expect(write).toHaveBeenLastCalledWith('/pairings', { name: '测试电脑' });
  });

  it('renders real empty pool states', () => {
    const view = render(<Blocks client={client} refresh={refresh} snapshot={centerSnapshot} navigate={vi.fn()} />);
    expect(screen.getByText('暂无阻塞记录')).toBeTruthy();
    view.unmount();
    render(<Agents snapshot={centerSnapshot} client={client} refresh={refresh} navigate={vi.fn()} />);
    expect(screen.getByText('尚无 Agent 节点')).toBeTruthy();
  });
});
