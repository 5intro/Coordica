import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { Candidate, Decision, Snapshot } from '../api.generated';
import { ApiClient, ApiError } from '../api/client';
import { centerSnapshot } from '../test/connectionInfo';
import { localRun, localTask, nodeSnapshot } from '../test/nodeSnapshot';
import { Automation } from './Automation';
import { Planning, emptyGoal } from './Planning';
import { NodeConfigure } from './NodeConfigure';
import { DecisionResolve } from './DecisionResolve';
import { Candidates } from './Candidates';
import { Agents } from './Agents';
import { Blocks } from './Blocks';
import { blockRecords } from './management';
const goal = { ...emptyGoal, version: 7, authorization_version: 3, approved_version: 7, approved_authorization_version: 2, objective: 'Build', acceptance: 'Checks', allowed_roots: ['web/src'], budget_seconds: 100 };
const snapshot: Snapshot = { ...centerSnapshot, automation: { ...centerSnapshot.automation, enabled: true }, planning_document: goal, requirements: [{ id: 'req', version: 1, title: 'Existing requirement', body: '', created_at: '' }] };
const decision: Decision = { id: 'd', version: 4, source_kind: 'planning_run', source_id: 'leader-run', source_version: 9, document_version: 7, authorization_version: 3, kind: 'lost', evidence: 'lease expired', actions: ['confirm_stopped'], status: 'pending', resolution: '' };
const candidate: Candidate = { baseline_version: 1, baseline_digest: 'fixture', id: 'current', version: 1, task_id: localTask.id, source_run_id: localRun.id, authorization_version: 1, policy_version: centerSnapshot.project!.policy_version, base: 'B', head: 'H', main: 'M', commit: 'C', state: 'ready_to_land', detail: '', protected: true, approved_commit: '', checks: [], created_at: '' };
function controls() { const client = new ApiClient('owner'); return { client, refresh: vi.fn(), write: vi.spyOn(client, 'write').mockResolvedValue({}) }; }
describe('Phase B owner writes and authority fences', () => {
  it('creates a goal from no document and preserves literal roots, empty architecture and selected requirement IDs', async () => {
    const { client, refresh, write } = controls();
    render(<Planning snapshot={{ ...snapshot, planning_document: null }} client={client} refresh={refresh} />);
    fireEvent.click(screen.getByRole('button', { name: '创建目标' }));
    fireEvent.click(screen.getByRole('button', { name: '保存目标' }));
    expect(write).not.toHaveBeenCalled();
    expect(screen.getByRole('alert').textContent).toContain('请填写');
    fireEvent.change(screen.getByLabelText('目标'), { target: { value: 'Build' } });
    fireEvent.change(screen.getByLabelText('验收标准'), { target: { value: 'Checks' } });
    fireEvent.change(screen.getByLabelText(/字面允许根目录（每行一个）/), { target: { value: 'web/src\n literal root ' } });
    fireEvent.click(screen.getByLabelText('Existing requirement · req'));
    fireEvent.click(screen.getByRole('button', { name: '保存目标' }));
    await waitFor(() => expect(write).toHaveBeenCalledWith('/planning/save', { expected_version: 0, document: { ...emptyGoal, objective: 'Build', acceptance: 'Checks', requirement_ids: ['req'], allowed_roots: ['web/src', ' literal root '] } }));
  });
  it('saves against the edited version without stripping authorization fields and refreshes stale errors', async () => {
    const { client, refresh, write } = controls();
    write.mockRejectedValue(new ApiError('stale_version', '目标已变化', 409));
    render(<Planning snapshot={snapshot} client={client} refresh={refresh} />);
    fireEvent.click(screen.getByRole('button', { name: '编辑目标' }));
    fireEvent.change(screen.getByLabelText('目标'), { target: { value: 'Changed' } });
    fireEvent.click(screen.getByRole('button', { name: '保存目标' }));
    await screen.findByText('目标已变化');
    expect(write).toHaveBeenCalledWith('/planning/save', { expected_version: 7, document: { ...goal, objective: 'Changed' } });
    expect(screen.getByLabelText('目标')).toHaveProperty('value', 'Changed');
    fireEvent.click(screen.getByRole('button', { name: '刷新规划状态' }));
    expect(refresh).toHaveBeenCalled();
  });
  it('sends authorize, message, evaluate and cancel versions and never replaces run state with a receipt', async () => {
    const { client, refresh, write } = controls();
    render(<Planning snapshot={{ ...snapshot, planning_runs: [{ baseline_version: 1, baseline_digest: 'fixture', scope_request_id: '', id: 'leader-run', execution_adapter: 'codex_cli', process_id: 123, working_directory: '/work', task_mapping: [], version: 1, document_version: 7, authorization_version: 3, automation_version: 1, generation: 1, state: 'lost', attempt: 1, max_retries: 2, grant_seconds: 30, elapsed_seconds: 12, lease_until: '', deadline: '', started_at: '', tree_stopped: false, settled: false, session_id: '', detail: '', task_ids: [] }] }} client={client} refresh={refresh} />);
    fireEvent.click(screen.getByRole('button', { name: 'Owner 一次授权' }));
    await waitFor(() => expect(write).toHaveBeenCalledWith('/planning/authorize', { expected_version: 7 }));
    fireEvent.click(screen.getByRole('button', { name: '发送并执行评估' }));
    expect(write).toHaveBeenCalledTimes(1);
    fireEvent.change(screen.getByLabelText('发送给 Leader'), { target: { value: 'Clarify' } });
    fireEvent.click(screen.getByRole('button', { name: '发送并执行评估' }));
    await waitFor(() => expect(write).toHaveBeenCalledWith('/planning/messages', { expected_version: 7, text: 'Clarify' }));
    fireEvent.click(screen.getByRole('button', { name: '评估规划' }));
    await waitFor(() => expect(write).toHaveBeenCalledWith('/planning/evaluate', { expected_version: 7, text: 'Clarify' }));
    fireEvent.click(screen.getByRole('button', { name: '请求取消规划' }));
    await waitFor(() => expect(write).toHaveBeenCalledWith('/planning/cancel', { expected_version: 7 }));
    expect(screen.getByText(/进程树未确认停止/)).toBeTruthy();
    expect(screen.getByText('失联')).toBeTruthy();
    expect(screen.getByText(/执行适配器 codex_cli/)).toBeTruthy();
  });
  it('authorizes the current goal with automation off without inventing approval or admission', async () => {
    const { client, refresh, write } = controls();
    const state = { ...snapshot, automation: { ...snapshot.automation, enabled: false } };
    const view = render(<Planning snapshot={state} client={client} refresh={refresh} />);
    expect(screen.getByText(/Owner 一次授权独立于自动化开关/)).toBeTruthy();
    const newerGoal = { ...goal, version: 8 };
    view.rerender(<Planning snapshot={{ ...state, planning_document: newerGoal }} client={client} refresh={refresh} />);
    fireEvent.click(screen.getByRole('button', { name: 'Owner 一次授权' }));
    await waitFor(() => expect(refresh).toHaveBeenCalled());
    expect(write).toHaveBeenCalledExactlyOnceWith('/planning/authorize', { expected_version: 8 });
    expect(screen.getByText(/文档 v8 · 授权 v3 · 已批准文档 v7 \/ 授权 v2 · 准入文档 v0 \/ 授权 v0/)).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Owner 一次授权' })).toHaveProperty('disabled', false);
    view.rerender(<Planning snapshot={{ ...state, planning_document: { ...newerGoal, approved_version: 8, approved_authorization_version: 3 } }} client={client} refresh={refresh} />);
    expect(screen.getByRole('button', { name: 'Owner 一次授权' })).toHaveProperty('disabled', true);
  });
  it('hides an open planning draft and chat if the project disappears', () => {
    const { client, refresh, write } = controls();
    const view = render(<Planning snapshot={snapshot} client={client} refresh={refresh} />);
    fireEvent.click(screen.getByRole('button', { name: '编辑目标' }));
    expect(screen.getByRole('button', { name: '保存目标' })).toBeTruthy();
    view.rerender(<Planning snapshot={{ ...snapshot, project: null }} client={client} refresh={refresh} />);
    expect(screen.queryByRole('button', { name: '保存目标' })).toBeNull();
    expect(screen.queryByLabelText('发送给 Leader')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Owner 一次授权' })).toBeNull();
    expect(write).not.toHaveBeenCalled();
  });
  it('keeps automation default off, validates runtime input and writes only trusted tool/model settings', async () => {
    const { client, refresh, write } = controls();
    render(<Automation settings={centerSnapshot.automation} client={client} refresh={refresh} />);
    const enabled = screen.getByLabelText('自动化开关');
    expect(enabled).toHaveProperty('checked', false);
    expect(screen.queryByText(/Astra/)).toBeNull();
    fireEvent.change(screen.getByLabelText('规划 / 初始化最大重试次数'), { target: { value: '' } });
    fireEvent.click(screen.getByRole('button', { name: '保存系统自动化' }));
    expect(write).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText('规划 / 初始化最大重试次数'), { target: { value: '3' } });
    fireEvent.click(screen.getByRole('button', { name: '保存系统自动化' }));
    expect(write).not.toHaveBeenCalled();
    expect(screen.getByRole('alert').textContent).toContain('0–2');
    fireEvent.change(screen.getByLabelText('规划 / 初始化最大重试次数'), { target: { value: '2' } });
    fireEvent.click(enabled);
    fireEvent.click(screen.getByRole('button', { name: '保存系统自动化' }));
    await waitFor(() => expect(write).toHaveBeenCalledWith('/system/automation', { expected_version: 1, settings: { ...centerSnapshot.automation, enabled: true, planning_max_retries: 2 } }));
    expect(screen.getByText(/当前中心设置/).textContent).toContain('关闭');
  });
  it('configures the union of reported and stored tools with capacity and scheduling permissions', async () => {
    const { client, refresh, write } = controls();
    const node = { ...nodeSnapshot.node, tools: [{ tool: 'old', enabled: true, implement: true, review: false }] };
    render(<NodeConfigure node={node} client={client} refresh={refresh} />);
    fireEvent.click(screen.getByRole('button', { name: '配置调度权限' }));
    fireEvent.change(screen.getByLabelText('节点容量'), { target: { value: '3' } });
    fireEvent.click(screen.getByRole('button', { name: '保存节点配置' }));
    expect(write).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText('节点容量'), { target: { value: '2' } });
    fireEvent.click(screen.getByLabelText('排空'));
    fireEvent.click(screen.getByLabelText('dsh enabled'));
    fireEvent.click(screen.getByRole('button', { name: '保存节点配置' }));
    await waitFor(() => expect(write).toHaveBeenCalledWith('/nodes/node_local/configure', { expected_version: 1, name: node.name, capacity: 2, draining: true, disabled_new_work: false, tools: [{ tool: 'dsh', enabled: true, implement: false, review: false }, node.tools[0]] }));
  });
  it('preserves node drafts on stale requests and refreshes without inventing permissions', async () => {
    const { client, refresh, write } = controls();
    write.mockRejectedValue(new ApiError('stale_version', '节点版本已变化', 409));
    render(<NodeConfigure node={nodeSnapshot.node} client={client} refresh={refresh} />);
    fireEvent.click(screen.getByRole('button', { name: '配置调度权限' }));
    fireEvent.change(screen.getByLabelText('节点名称'), { target: { value: 'Renamed' } });
    fireEvent.click(screen.getByRole('button', { name: '保存节点配置' }));
    await screen.findByText('节点版本已变化');
    expect(screen.getByLabelText('节点名称')).toHaveProperty('value', 'Renamed');
    fireEvent.click(screen.getByRole('button', { name: '刷新节点版本' }));
    expect(refresh).toHaveBeenCalled();
    expect(write).toHaveBeenCalledWith('/nodes/node_local/configure', expect.objectContaining({ expected_version: 1, name: 'Renamed' }));
  });
  it('never attaches an edited automation draft to a newer version without reloading', async () => {
    const { client, refresh, write } = controls();
    const view = render(<Automation settings={snapshot.automation} client={client} refresh={refresh} />);
    fireEvent.click(screen.getByLabelText('自动化开关'));
    const newer = { ...snapshot.automation, version: 2, planning_budget_seconds: 1200 };
    view.rerender(<Automation settings={newer} client={client} refresh={refresh} />);
    fireEvent.click(screen.getByRole('button', { name: '保存系统自动化' }));
    expect(write).not.toHaveBeenCalled();
    expect(screen.getByRole('alert').textContent).toContain('版本已变化');
    fireEvent.click(screen.getByRole('button', { name: '载入最新设置' }));
    fireEvent.click(screen.getByRole('button', { name: '保存系统自动化' }));
    await waitFor(() => expect(write).toHaveBeenCalledWith('/system/automation', { expected_version: 2, settings: newer }));
  });
  it('keeps a decision edit tied to its reviewed source version across snapshot updates', async () => {
    const { client, refresh, write } = controls();
    const view = render(<DecisionResolve decision={decision} client={client} refresh={refresh} />);
    fireEvent.change(screen.getByLabelText('决策操作 d'), { target: { value: 'confirm_stopped' } });
    fireEvent.change(screen.getByLabelText('决策证据 d'), { target: { value: 'PID verified' } });
    view.rerender(<DecisionResolve decision={{ ...decision, source_version: 10 }} client={client} refresh={refresh} />);
    fireEvent.click(screen.getByRole('button', { name: '提交决策处理' }));
    expect(write).not.toHaveBeenCalled();
    expect(screen.getByRole('alert').textContent).toContain('来源版本已变化');
    fireEvent.click(screen.getByRole('button', { name: '载入当前决策' }));
    expect(screen.getByLabelText('决策证据 d')).toHaveProperty('value', '');
  });
  it('closing automation writes disabled settings without cancelling authorized work', async () => {
    const { client, refresh, write } = controls();
    render(<Automation settings={snapshot.automation} client={client} refresh={refresh} />);
    fireEvent.click(screen.getByLabelText('自动化开关'));
    fireEvent.click(screen.getByRole('button', { name: '保存系统自动化' }));
    await waitFor(() => expect(write).toHaveBeenCalledWith('/system/automation', { expected_version: 1, settings: { ...snapshot.automation, enabled: false } }));
    expect(write).toHaveBeenCalledTimes(1);
  });
  it('requires real stop evidence, preserves source version and surfaces denied resolution', async () => {
    const { client, refresh, write } = controls();
    write.mockRejectedValue(new ApiError('forbidden', 'Owner denied', 403));
    render(<DecisionResolve decision={decision} client={client} refresh={refresh} />);
    fireEvent.change(screen.getByLabelText('决策操作 d'), { target: { value: 'confirm_stopped' } });
    expect(screen.getByText(/Owner 必须亲自核验/).textContent).toContain('原始进程身份及进程树');
    expect(screen.getByText(/Owner 必须亲自核验/).textContent).toContain('核对来源 Run 的版本');
    fireEvent.change(screen.getByLabelText('决策证据 d'), { target: { value: '   ' } });
    fireEvent.click(screen.getByRole('button', { name: '提交决策处理' }));
    expect(write).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText('决策证据 d'), { target: { value: 'Verified PID 123 and descendants exited' } });
    fireEvent.click(screen.getByRole('button', { name: '提交决策处理' }));
    await screen.findByText('Owner denied');
    expect(write).toHaveBeenCalledWith('/decisions/d/resolve', { expected_version: 4, expected_source_version: 9, action: 'confirm_stopped', evidence: 'Verified PID 123 and descendants exited' });
    expect(refresh).toHaveBeenCalled();
  });
  it('requires original Run stop proof for confirm_run_stopped and only acknowledges the decision request', async () => {
    const { client, refresh, write } = controls();
    const scopeDecision = { ...decision, kind: 'scope_request', source_kind: 'run', source_id: 'original-run', actions: ['confirm_run_stopped'] };
    render(<DecisionResolve decision={scopeDecision} client={client} refresh={refresh} />);
    fireEvent.change(screen.getByLabelText('决策操作 d'), { target: { value: 'confirm_run_stopped' } });
    const caution = screen.getByText(/Owner 必须亲自核验/).textContent;
    expect(caution).toContain('原始进程身份及进程树');
    expect(caution).toContain('核对来源 Run 的版本');
    expect(caution).toContain('失联、取消请求和模型文字都不是停止证据');
    expect(caution).toContain('必须先在原始 Run 通过共享停止证据 API 成功记录停止证明');
    for (const evidence of ['', '   ']) {
      fireEvent.change(screen.getByLabelText('决策证据 d'), { target: { value: evidence } });
      fireEvent.click(screen.getByRole('button', { name: '提交决策处理' }));
      expect(write).not.toHaveBeenCalled();
      expect(screen.getByRole('alert').textContent).toContain('进程身份及进程树核验依据');
    }
    fireEvent.change(screen.getByLabelText('决策证据 d'), { target: { value: 'Original Run stop proof recorded after checking PID and descendants' } });
    fireEvent.click(screen.getByRole('button', { name: '提交决策处理' }));
    await screen.findByText('决策处理请求已受理；此回执不代表当前运行已停止，以中心状态为准。');
    expect(write).toHaveBeenCalledTimes(1);
    expect(write).toHaveBeenCalledWith('/decisions/d/resolve', { expected_version: 4, expected_source_version: 9, action: 'confirm_run_stopped', evidence: 'Original Run stop proof recorded after checking PID and descendants' });
    expect(refresh).toHaveBeenCalled();
  });
  it('reloads a conflicted stop decision before sending the current decision and source versions', async () => {
    const { client, refresh, write } = controls();
    const scopeDecision = { ...decision, kind: 'scope_request', source_kind: 'run', source_id: 'original-run', actions: ['confirm_run_stopped'] };
    write.mockRejectedValueOnce(new ApiError('conflict', 'Run version changed', 409));
    const view = render(<DecisionResolve decision={scopeDecision} client={client} refresh={refresh} />);
    fireEvent.change(screen.getByLabelText('决策操作 d'), { target: { value: 'confirm_run_stopped' } });
    fireEvent.change(screen.getByLabelText('决策证据 d'), { target: { value: 'Reviewed original Run proof' } });
    fireEvent.click(screen.getByRole('button', { name: '提交决策处理' }));
    await screen.findByText('Run version changed');
    expect(refresh).toHaveBeenCalled();
    view.rerender(<DecisionResolve decision={{ ...scopeDecision, version: 5, source_version: 10 }} client={client} refresh={refresh} />);
    fireEvent.click(screen.getByRole('button', { name: '提交决策处理' }));
    expect(write).toHaveBeenCalledTimes(1);
    expect(screen.getByRole('alert').textContent).toContain('来源版本已变化');
    fireEvent.click(screen.getByRole('button', { name: '载入当前决策' }));
    expect(screen.getByLabelText('决策操作 d')).toHaveProperty('value', '');
    expect(screen.getByLabelText('决策证据 d')).toHaveProperty('value', '');
    fireEvent.change(screen.getByLabelText('决策操作 d'), { target: { value: 'confirm_run_stopped' } });
    fireEvent.change(screen.getByLabelText('决策证据 d'), { target: { value: 'Rechecked original Run proof and version' } });
    fireEvent.click(screen.getByRole('button', { name: '提交决策处理' }));
    await screen.findByText('决策处理请求已受理；此回执不代表当前运行已停止，以中心状态为准。');
    expect(write).toHaveBeenCalledTimes(2);
    expect(write).toHaveBeenLastCalledWith('/decisions/d/resolve', { expected_version: 5, expected_source_version: 10, action: 'confirm_run_stopped', evidence: 'Rechecked original Run proof and version' });
  });
  it.each(['evaluate', 'edit_goal', 'edit_budget', 'dismiss'])('acknowledges %s as a choice requiring appropriate source operations', async (selected) => {
    const { client, refresh, write } = controls();
    render(<DecisionResolve decision={{ ...decision, actions: [selected] }} client={client} refresh={refresh} />);
    fireEvent.change(screen.getByLabelText('决策操作 d'), { target: { value: selected } });
    expect(screen.getByText(/主要记录决策选择/).textContent).toContain('本身不会编辑或授权目标，也不会改变预算');
    fireEvent.change(screen.getByLabelText('决策证据 d'), { target: { value: 'Owner reviewed the source' } });
    fireEvent.click(screen.getByRole('button', { name: '提交决策处理' }));
    await screen.findByText('决策选择已记录；目标编辑、目标授权或预算调整仍须按需在来源对象执行，以中心状态为准。');
    expect(write).toHaveBeenCalledTimes(1);
    expect(write).toHaveBeenCalledWith('/decisions/d/resolve', { expected_version: 4, expected_source_version: 9, action: selected, evidence: 'Owner reviewed the source' });
  });
  it.each(['evaluate', 'edit_budget', 'dismiss', 'edit_goal'])('requires evidence for %s without issuing an empty request', (selected) => {
    const { client, refresh, write } = controls();
    render(<DecisionResolve decision={{ ...decision, actions: [selected] }} client={client} refresh={refresh} />);
    fireEvent.change(screen.getByLabelText('决策操作 d'), { target: { value: selected } });
    for (const value of ['', '   ']) {
      fireEvent.change(screen.getByLabelText('决策证据 d'), { target: { value } });
      fireEvent.click(screen.getByRole('button', { name: '提交决策处理' }));
      expect(write).not.toHaveBeenCalled();
      expect(screen.getByRole('alert').textContent).toContain('非空');
    }
  });
  it.each([
    ['规划 / 初始化超时（秒）', '0'], ['规划 / 初始化超时（秒）', '604801'], ['规划 / 初始化超时（秒）', '1.5'],
    ['规划累计预算（秒）', '1'], ['规划累计预算（秒）', '31536001'], ['规划累计预算（秒）', '900.5'],
    ['规划 / 初始化最大重试次数', '-1'], ['规划 / 初始化最大重试次数', '0.5'],
  ])('rejects out-of-contract automation %s=%s', (label, value) => {
    const { client, refresh, write } = controls();
    render(<Automation settings={centerSnapshot.automation} client={client} refresh={refresh} />);
    fireEvent.change(screen.getByLabelText(label), { target: { value } });
    fireEvent.submit(screen.getByRole('button', { name: '保存系统自动化' }).closest('form')!);
    expect(write).not.toHaveBeenCalled();
    expect(screen.getByRole('alert').textContent).toContain('31536000');
  });
  it('never falls back to another candidate for an explicit missing deep link', () => {
    const { client, refresh } = controls();
    render(<Candidates snapshot={{ ...snapshot, candidates: [candidate] }} client={client} refresh={refresh} initialId="missing" />);
    expect(screen.getByText('候选不存在')).toBeTruthy();
    expect(screen.queryByRole('button', { name: '特别批准此 C' })).toBeNull();
  });
  it('excludes superseded or stale candidate blocks and deduplicates server decisions', () => {
    const currentTask = { ...localTask, version: 8, approved_version: 2, state: 'awaiting_integration', current_candidate_id: candidate.id };
    const state = { ...snapshot, tasks: [currentTask], candidates: [candidate, { ...candidate, id: 'historical' }] };
    expect(blockRecords(state).map((b) => b.id)).toEqual(['candidate:current']);
    const staleBlock = { kind: 'candidate', object_id: 'historical', reason: 'stale protected candidate', actions: ['approve-protected'] };
    expect(blockRecords({ ...state, blocks: [staleBlock] }).map((b) => b.id)).toEqual(['candidate:current']);
    expect(blockRecords({ ...state, candidates: [{ ...candidate, authorization_version: 2 }], blocks: [{ ...staleBlock, object_id: candidate.id }] })).toEqual([]);
    expect(blockRecords({ ...state, blocks: [{ ...staleBlock, object_id: candidate.id }] }).map((b) => b.id)).toEqual(['candidate:current']);
    for (const task of [{ ...currentTask, authorization_version: 2 }, { ...currentTask, approved_version: 0 }, { ...currentTask, current_candidate_id: 'other' }, { ...currentTask, state: 'cancelled' }]) expect(blockRecords({ ...state, tasks: [task] })).toEqual([]);
    expect(blockRecords({ ...state, tasks: [{ ...currentTask, hold_reason: 'budget' }] }).map((b) => b.id)).toEqual([`task:${currentTask.id}`]);
    expect(blockRecords({ ...state, project: { ...centerSnapshot.project!, policy_version: candidate.policy_version + 1 } })).toEqual([]);
    expect(blockRecords({ ...snapshot, decisions: [decision], blocks: [{ kind: 'planning_run', object_id: decision.source_id, reason: 'lost', actions: ['confirm_stopped'] }] })).toHaveLength(1);
  });
  it('preserves removed tool navigation for nonquiescent lost runs and distinguishes unknown capability', () => {
    const { client, refresh } = controls();
    const navigate = vi.fn();
    render(<Agents snapshot={{ ...snapshot, nodes: [{ ...nodeSnapshot.node, capabilities: [] }], tasks: [localTask], runs: [{ ...localRun, state: 'lost' }] }} client={client} refresh={refresh} navigate={navigate} />);
    expect(screen.getByText('工具未再报告 · 可用性未知')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /失联 · 未确认静止/ }));
    expect(navigate).toHaveBeenCalledWith('runs', localRun.id);
    fireEvent.change(screen.getByLabelText('Agent 可用性'), { target: { value: 'unavailable' } });
    expect(screen.getByText('没有匹配 Agent')).toBeTruthy();
  });
  it('shows resolved evidence and actual source navigation without repeating logical rows', () => {
    const { client, refresh } = controls(); const navigate = vi.fn();
    render(<Blocks snapshot={{ ...snapshot, decisions: [decision, { ...decision, id: 'resolved', status: 'resolved', resolution: 'confirmed with evidence' }], blocks: [{ kind: 'planning_run', object_id: decision.source_id, reason: 'lost', actions: [] }] }} client={client} refresh={refresh} navigate={navigate} />);
    expect(screen.getAllByRole('button', { name: '提交决策处理' })).toHaveLength(1);
    expect(screen.getByText('处理：confirmed with evidence')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '来源 v9 →' }));
    expect(navigate).toHaveBeenCalledWith('planning', 'leader-run');
  });
});

it.each([[3601, 14401], [604800, 31536000]])('allows finite long planning timeout %i and cumulative budget %i', async (timeout, budget) => {
  const client = new ApiClient('owner');
  const write = vi.spyOn(client, 'write').mockResolvedValue({});
  render(<Automation settings={centerSnapshot.automation} client={client} refresh={vi.fn()} />);
  fireEvent.change(screen.getByLabelText('规划 / 初始化超时（秒）'), { target: { value: String(timeout) } });
  fireEvent.change(screen.getByLabelText('规划累计预算（秒）'), { target: { value: String(budget) } });
  fireEvent.submit(screen.getByRole('button', { name: '保存系统自动化' }).closest('form')!);
  await waitFor(() => expect(write).toHaveBeenCalledWith('/system/automation', expect.objectContaining({ settings: expect.objectContaining({ planning_timeout_seconds: timeout, planning_budget_seconds: budget }) })));
});
