import { fireEvent, render, screen, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { Candidate, Snapshot } from '../api.generated';
import { centerSnapshot } from '../test/connectionInfo';
import { localRun, localTask, nodeSnapshot } from '../test/nodeSnapshot';
import { emptyGoal } from './Planning';
import { Overview } from './Overview';

const candidate: Candidate = { baseline_version: 1, baseline_digest: 'fixture', id: 'candidate-fixed', version: 2, task_id: localTask.id, source_run_id: localRun.id, authorization_version: 1, policy_version: 1, base: 'B', head: 'H', main: 'M', commit: 'fixed-C', state: 'reviewing', detail: '', protected: false, approved_commit: '', checks: [], created_at: '' };
const show = (snapshot: Snapshot = centerSnapshot) => {
  const navigate = vi.fn();
  render(<Overview snapshot={snapshot} navigate={navigate} />);
  return navigate;
};

describe('Overview factual work lists', () => {
  it('shows project, saved goal, pause state and integrated progress with a requirement entry', () => {
    const navigate = show({ ...centerSnapshot, project: { ...centerSnapshot.project!, paused: true }, planning_document: { ...emptyGoal, objective: '交付真实工作列表' }, tasks: [{ ...localTask, state: 'integrated' }, { ...localTask, id: 'queued', state: 'ready' }] });
    expect(screen.getByRole('heading', { level: 1, name: '协作项目' })).toBeTruthy();
    expect(screen.getByText('交付真实工作列表')).toBeTruthy();
    expect(screen.getByText('已暂停')).toBeTruthy();
    expect(screen.getByText('任务进度：1 / 2 已集成')).toBeTruthy();
    fireEvent.click(screen.getByRole('link', { name: '提交需求' }));
    expect(navigate).toHaveBeenCalledWith('requirements', undefined);
    expect(screen.queryByRole('region', { name: '人工待办' })).toBeNull();
  });

  it('collapses human work for ordinary holds and running work; shows actionable drafts and lost runs', () => {
    const view = render(<Overview snapshot={{ ...centerSnapshot, tasks: [{ ...localTask, hold_reason: '等待依赖' }], runs: [localRun] }} navigate={vi.fn()} />);
    expect(screen.queryByRole('region', { name: '人工待办' })).toBeNull();
    const navigate = vi.fn();
    view.rerender(<Overview snapshot={{ ...centerSnapshot, tasks: [{ ...localTask, state: 'draft' }], runs: [{ ...localRun, state: 'lost' }] }} navigate={navigate} />);
    const todos = within(screen.getByRole('region', { name: '人工待办' }));
    expect(todos.getAllByRole('button')).toHaveLength(2);
    fireEvent.click(todos.getByRole('button', { name: /待批准/ }));
    expect(navigate).toHaveBeenCalledWith('tasks', localTask.id);
    fireEvent.click(todos.getByRole('button', { name: /未确认静止/ }));
    expect(navigate).toHaveBeenCalledWith('runs', localRun.id);
    view.rerender(<Overview snapshot={{ ...centerSnapshot, runs: [{ ...localRun, state: 'lost', quiescent: true }] }} navigate={navigate} />);
    expect(screen.queryByRole('region', { name: '人工待办' })).toBeNull();
  });

  it('deduplicates open decisions and removes resolved decisions from human work', () => {
    const decision = { id: 'decision-budget', version: 1, source_kind: 'task', source_id: localTask.id, source_version: 1, document_version: 1, authorization_version: 1, kind: 'budget_exhausted', evidence: '累计预算已用尽', actions: ['edit_budget'], status: 'pending', resolution: '' };
    const snapshot: Snapshot = { ...centerSnapshot, tasks: [{ ...localTask, hold_reason: '累计预算已用尽' }], decisions: [decision], blocks: [{ kind: 'task', object_id: localTask.id, reason: '累计预算已用尽', actions: ['edit_budget'] }] };
    const navigate = vi.fn();
    const view = render(<Overview snapshot={snapshot} navigate={navigate} />);
    const todos = within(screen.getByRole('region', { name: '人工待办' }));
    expect(todos.getAllByRole('button')).toHaveLength(1);
    fireEvent.click(todos.getByRole('button'));
    expect(navigate).toHaveBeenCalledWith('blocks', decision.id);
    expect(within(screen.getByRole('region', { name: '阻断池' })).getByLabelText('阻断池活动总数').textContent).toBe('1');
    view.rerender(<Overview snapshot={{ ...snapshot, decisions: [{ ...decision, status: 'resolved' }] }} navigate={navigate} />);
    expect(screen.queryByRole('region', { name: '人工待办' })).toBeNull();
  });

  it('uses four real pools with activity counts, five-row previews and source navigation', () => {
    const requirements = Array.from({ length: 7 }, (_, index) => ({ id: `req-${index}`, version: 1, title: `需求 ${index}`, body: '原文', created_at: '' }));
    const tasks = Array.from({ length: 7 }, (_, index) => ({ ...localTask, id: `task-${index}`, title: `工作 ${index}`, requirement_ids: [`req-${index}`], state: index === 6 ? 'integrated' : 'ready', hold_reason: index === 0 ? '需要预算调整' : '' }));
    const navigate = show({ ...centerSnapshot, requirements, tasks, candidates: [{ ...candidate, task_id: 'task-1' }, { ...candidate, id: 'history', state: 'landed' }] });
    for (const name of ['需求池', '任务池']) {
      const pool = within(screen.getByRole('region', { name }));
      expect(pool.getByLabelText(`${name}活动总数`).textContent).toBe('6');
      expect(pool.getAllByRole('button')).toHaveLength(5);
      expect(pool.getByRole('link', { name: '查看全部' })).toBeTruthy();
    }
    const reviews = within(screen.getByRole('region', { name: '审查池' }));
    expect(reviews.getByLabelText('审查池活动总数').textContent).toBe('1');
    expect(reviews.getByText(/固定版本 fixed-C/)).toBeTruthy();
    fireEvent.click(reviews.getByRole('button'));
    expect(navigate).toHaveBeenCalledWith('candidates', candidate.id);
    const blocks = within(screen.getByRole('region', { name: '阻断池' }));
    expect(blocks.getByText('需要预算调整')).toBeTruthy();
    expect(blocks.getByLabelText('阻断池活动总数').textContent).toBe('1');
    fireEvent.click(blocks.getByRole('button'));
    expect(navigate).toHaveBeenCalledWith('tasks', 'task-0');
    fireEvent.click(within(screen.getByRole('region', { name: '需求池' })).getByRole('link', { name: '查看全部' }));
    expect(navigate).toHaveBeenCalledWith('requirements', undefined);
  });

  it('distinguishes prechecks from Agent review and only requests protected approval when ready', () => {
    const snapshot: Snapshot = { ...centerSnapshot, tasks: [{ ...localTask, state: 'awaiting_integration', current_candidate_id: candidate.id }], candidates: [{ ...candidate, protected: true, state: 'prechecking' }] };
    const view = render(<Overview snapshot={snapshot} navigate={vi.fn()} />);
    expect(within(screen.getByRole('region', { name: '审查池' })).getByText('预检查 · 尚未进入 Agent 审查')).toBeTruthy();
    expect(screen.queryByRole('region', { name: '人工待办' })).toBeNull();
    view.rerender(<Overview snapshot={{ ...snapshot, candidates: [{ ...snapshot.candidates![0], state: 'reviewing' }] }} navigate={vi.fn()} />);
    expect(screen.getByText('待审 / 审中')).toBeTruthy();
    expect(screen.queryByRole('region', { name: '人工待办' })).toBeNull();
    view.rerender(<Overview snapshot={{ ...snapshot, candidates: [{ ...snapshot.candidates![0], state: 'ready_to_land' }] }} navigate={vi.fn()} />);
    expect(within(screen.getByRole('region', { name: '人工待办' })).getByText('受保护变化等待固定 C 批准')).toBeTruthy();
    view.rerender(<Overview snapshot={{ ...snapshot, candidates: [{ ...snapshot.candidates![0], state: 'ready_to_land', approved_commit: candidate.commit }] }} navigate={vi.fn()} />);
    expect(screen.queryByRole('region', { name: '人工待办' })).toBeNull();
  });

  it('groups actual instances by host, keeps unknown and lost hosts and never instantiates capabilities', () => {
    const navigate = show({ ...centerSnapshot, tasks: [localTask], nodes: [nodeSnapshot.node, { ...nodeSnapshot.node, id: 'idle', name: '空闲主机', revoked: true }], runs: [localRun, { ...localRun, id: 'review-run', kind: 'review' }, { ...localRun, id: 'lost-run', node_id: 'unknown-host', state: 'lost' }, { ...localRun, id: 'finished', state: 'succeeded' }] });
    const hosts = within(screen.getByRole('complementary', { name: '主机运行动态' }));
    const active = within(hosts.getByRole('region', { name: nodeSnapshot.node.name }));
    expect(active.getAllByRole('button')).toHaveLength(2);
    expect(active.getByText('dsh · 独立审查 · 12 秒')).toBeTruthy();
    fireEvent.click(active.getByRole('button', { name: /实例 review-run/ }));
    expect(navigate).toHaveBeenCalledWith('runs', 'review-run');
    const idle = within(hosts.getByRole('region', { name: '空闲主机' }));
    expect(idle.getByText('已撤销')).toBeTruthy();
    expect(idle.getByText('暂无运行实例')).toBeTruthy();
    expect(idle.queryAllByRole('button')).toHaveLength(0);
    const unknown = within(hosts.getByRole('region', { name: 'unknown-host' }));
    expect(unknown.getByText('失联 · 未确认静止')).toBeTruthy();
    expect(hosts.queryByText(/实例 finished/)).toBeNull();
  });

  it('shows truthful empty pools, idle hosts and unknown goals', () => {
    show({ ...centerSnapshot, nodes: [nodeSnapshot.node] });
    for (const text of ['暂无活动需求', '暂无活动任务', '暂无活动候选', '暂无阻断', '尚未记录目标', '空闲 · 无活动运行记录']) expect(screen.getByText(text)).toBeTruthy();
    expect(screen.queryByRole('region', { name: '人工待办' })).toBeNull();
  });

  it('shows no-host and no-project states without fabricating work', () => {
    const view = render(<Overview snapshot={centerSnapshot} navigate={vi.fn()} />);
    expect(screen.getByText('尚无主机或运行记录')).toBeTruthy();
    view.rerender(<Overview snapshot={{ ...centerSnapshot, project: null }} navigate={vi.fn()} />);
    expect(screen.getByRole('heading', { name: '尚无项目' })).toBeTruthy();
  });
});
