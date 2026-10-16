import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { App } from './App';
import { ApiClient } from './api/client';
import { LocalClient } from './api/localClient';
import type { Candidate, Requirement, Snapshot } from './api.generated';
import { TOKEN_KEY } from './features/Login';
import { centerSnapshot, connectionInfo } from './test/connectionInfo';
import { localTask } from './test/nodeSnapshot';

const requirements: Requirement[] = [
  { id: 'r1', version: 1, title: '原始需求甲', body: '原文甲', assistant_summary: '摘要甲', related_ids: ['r2'], created_at: '2026-10-10' },
  { id: 'r2', version: 1, title: '关联需求乙', body: '原文乙', created_at: '2026-10-11' },
];
const candidate: Candidate = { id: 'c1', version: 1, task_id: localTask.id, source_run_id: 'run1', authorization_version: 1, policy_version: 1, baseline_version: 1, baseline_digest: 'baseline', base: 'b', head: 'h', main: 'm', commit: 'a'.repeat(40), state: 'ready_to_land', detail: '', protected: false, approved_commit: '', checks: [{ name: 'quick', passed: true, exit_code: 0, output: 'bounded output', duration_seconds: 1 }], review: { candidate: 'a'.repeat(40), verdict: 'approve', summary: '独立审查通过', findings: [], run_id: 'review1' }, created_at: '2026-10-11' };
const snapshot: Snapshot = { ...centerSnapshot, requirements, tasks: [{ ...localTask, requirement_ids: ['r1'], title: '开发任务甲' }, { ...localTask, id: 'task2', title: '开发任务乙' }], candidates: [candidate, { ...candidate, id: 'old', commit: 'b'.repeat(40), state: 'failed', created_at: '2026-10-09' }] };
function start(hash: string, value = snapshot) {
  window.history.replaceState(null, '', `/${hash}`);
  sessionStorage.setItem(TOKEN_KEY, 'owner');
  vi.spyOn(LocalClient.prototype, 'info').mockResolvedValue({ mode: 'center' });
  vi.spyOn(ApiClient.prototype, 'snapshot').mockResolvedValue(value);
  vi.spyOn(ApiClient.prototype, 'read').mockResolvedValue({ ...connectionInfo, platform: 'darwin' });
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {});
  return render(<App />);
}
afterEach(() => { sessionStorage.clear(); window.history.replaceState(null, '', '/'); vi.unstubAllGlobals(); });

it('restores deep-linked requirement, query, sort and browser back/forward selection', async () => {
  start('#requirements?id=r2&query=需求&sort=title');
  await screen.findByRole('heading', { name: '关联需求乙' });
  expect(screen.getByLabelText('搜索需求')).toHaveProperty('value', '需求');
  expect(screen.getByLabelText('需求排序')).toHaveProperty('value', 'title');
  // r1 declares r2; reverse links must still be visible when r2 has no related_ids.
  fireEvent.click(screen.getByRole('button', { name: '原始需求甲 →' }));
  await screen.findByRole('heading', { name: '原始需求甲' });
  expect(window.location.hash).toContain('id=r1');
  expect(screen.getByText('原文甲')).toBeTruthy();
  expect(screen.getByText('摘要甲')).toBeTruthy();
  window.history.back();
  await screen.findByRole('heading', { name: '关联需求乙' });
  window.history.forward();
  await screen.findByRole('heading', { name: '原始需求甲' });
  expect(screen.getByLabelText('搜索需求')).toHaveProperty('value', '需求');
});

it('retains task filters and scroll across Dock navigation, including deep-link refresh input', async () => {
  start('#tasks?id=task2&query=乙&filter=active&sort=title');
  await screen.findByRole('heading', { name: '开发任务乙' });
  expect(screen.getByLabelText('搜索任务')).toHaveProperty('value', '乙');
  Object.defineProperty(window, 'scrollY', { configurable: true, value: 480 });
  fireEvent.scroll(window);
  fireEvent.click(screen.getByRole('button', { name: '测试中心' }));
  await screen.findByRole('heading', { name: '测试中心' });
  Object.defineProperty(window, 'scrollY', { configurable: true, value: 0 });
  fireEvent.click(screen.getByRole('button', { name: '任务池' }));
  await screen.findByRole('heading', { name: '开发任务乙' });
  expect(screen.getByLabelText('搜索任务')).toHaveProperty('value', '乙');
  expect(screen.getByLabelText('任务筛选')).toHaveProperty('value', 'active');
  expect(window.location.hash).toContain('sort=title');
  await waitFor(() => expect(window.scrollTo).toHaveBeenCalledWith({ top: 480, behavior: 'auto' }));
});

it('shows review evidence on fixed C, retains failures in history, and does not create regression actions', async () => {
  start('#candidates?id=c1');
  await screen.findByText('独立审查通过');
  expect(screen.getByRole('link', { name: '开发任务甲' }).getAttribute('href')).toBe(`#tasks?id=${localTask.id}`);
  expect(screen.getByText(/有效快速检查证据由中心保留/)).toBeTruthy();
  expect(screen.getByRole('link', { name: '审查永久链接' }).getAttribute('href')).toBe('#candidates?id=c1');
  expect(screen.queryByRole('button', { name: /回归/ })).toBeNull();
  fireEvent.change(screen.getByLabelText('审查筛选'), { target: { value: 'history' } });
  const list = screen.getByRole('region', { name: '候选列表' });
  fireEvent.click(within(list).getByRole('button'));
  expect(window.location.hash).toContain('id=old');
  expect(screen.getByText('历史评审结论 approve · 对象与当前 C 不同')).toBeTruthy();
  expect(screen.getByRole('link', { name: '审查永久链接' }).getAttribute('href')).toBe('#candidates?id=old');
});

it('distinguishes prechecking from reviewing and lists missing planned checks', async () => {
  start('#candidates?id=c1', { ...snapshot, candidates: [{ ...candidate, state: 'prechecking', review: null, checks: [], check_plan: { policy_version: 1, digest: 'pinned', checks: [{ name: 'quick', command: 'node', args: [], timeout_seconds: 30 }] } }] });
  await screen.findByText('预检查中 · 尚未进入 Agent 独立审查。');
  expect(screen.getByText('未执行 / 缺失：quick')).toBeTruthy();
  expect(screen.queryByText('评审通过')).toBeNull();
  expect(screen.queryByRole('button', { name: /回归/ })).toBeNull();
});

it('shows genuine system write failure and unsaved state without enabling automation on navigation', async () => {
  const write = vi.spyOn(ApiClient.prototype, 'write').mockRejectedValue(new Error('系统保存失败'));
  start('#settings');
  await screen.findByRole('heading', { name: '项目设置' });
  fireEvent.click(screen.getByRole('button', { name: '系统' }));
  expect(screen.getByLabelText('自动化开关')).toHaveProperty('checked', false);
  expect(write).not.toHaveBeenCalled();
  fireEvent.change(screen.getByLabelText('规划 / 初始化超时（秒）'), { target: { value: '200' } });
  expect(screen.getByText(/未保存：修改仅在当前表单/)).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: '保存系统自动化' }));
  await screen.findByRole('alert');
  expect(screen.getByRole('alert').textContent).toBe('系统保存失败');
  expect(write).toHaveBeenCalledWith('/system/automation', expect.objectContaining({ expected_version: centerSnapshot.automation.version, settings: expect.objectContaining({ enabled: false, planning_timeout_seconds: 200 }) }));
  expect(screen.getByText(/当前中心设置 v/).textContent).toContain('关闭');
});

it('keeps eight Dock destinations and arrow/Home/End keyboard focus', async () => {
  start('#overview');
  await screen.findByRole('heading', { name: centerSnapshot.project!.name });
  const dock = screen.getByRole('navigation', { name: '工作台页面' });
  const buttons = within(dock).getAllByRole('button');
  expect(buttons.map((button) => button.getAttribute('aria-label'))).toEqual(['总览', '需求池', '任务池', '审查池', '阻断池', 'Agent 管理', '测试中心', '设置']);
  // jsdom has no layout. Supply visibility for the keyboard handler only.
  buttons.forEach((button) => vi.spyOn(button, 'getClientRects').mockReturnValue([{}] as unknown as DOMRectList));
  buttons[0].focus(); fireEvent.keyDown(buttons[0], { key: 'ArrowRight' });
  expect(document.activeElement).toBe(buttons[1]);
  fireEvent.keyDown(buttons[1], { key: 'End' }); expect(document.activeElement).toBe(buttons[7]);
  fireEvent.keyDown(buttons[7], { key: 'Home' }); expect(document.activeElement).toBe(buttons[0]);
  expect(buttons[0].getAttribute('aria-current')).toBe('page');
});

it('keeps all pools reachable from the compact Dock without a duplicate pool switch', async () => {
  vi.stubGlobal('matchMedia', vi.fn().mockReturnValue({ matches: true, addEventListener: vi.fn(), removeEventListener: vi.fn() }));
  start('#requirements?id=r1');
  await screen.findByRole('heading', { name: '原始需求甲' });
  const dock = within(screen.getByRole('navigation', { name: '工作台页面' }));
  expect(dock.getAllByRole('button')).toHaveLength(8);
  expect(dock.getByRole('button', { name: '需求池' }).getAttribute('aria-current')).toBe('page');
  expect(screen.queryByRole('navigation', { name: '工作池切换' })).toBeNull();
  fireEvent.click(dock.getByRole('button', { name: '审查池' }));
  await screen.findByRole('heading', { name: '审查池' });
  expect(window.location.hash).toBe('#candidates');
});

it('reports system save acceptance separately from the refreshed effective version', async () => {
  start('#automation');
  const state = vi.mocked(ApiClient.prototype.snapshot);
  vi.spyOn(ApiClient.prototype, 'write').mockImplementation(async () => {
    state.mockResolvedValue({ ...snapshot, automation: { ...snapshot.automation, version: snapshot.automation.version + 1, planning_timeout_seconds: 200 } });
    return {} as never;
  });
  await screen.findByRole('button', { name: '保存系统自动化' });
  fireEvent.change(screen.getByLabelText('规划 / 初始化超时（秒）'), { target: { value: '200' } });
  fireEvent.click(screen.getByRole('button', { name: '保存系统自动化' }));
  await screen.findByText('系统设置保存请求已完成。');
  await waitFor(() => expect(screen.getByText(/当前中心设置 v/).textContent).toContain(`v${snapshot.automation.version + 1}`));
  expect(screen.getByText('当前显示已生效的中心设置。')).toBeTruthy();
  expect(screen.getByLabelText('自动化开关')).toHaveProperty('checked', false);
});

it('restores a block history link and filters historical evidence without presenting it as an active decision', async () => {
  start('#blocks?id=decision%3Aresolved&history=history&query=stop', { ...snapshot, decisions: [{ id: 'resolved', version: 1, source_kind: 'run', source_id: 'run1', source_version: 1, kind: 'stop', status: 'resolved', evidence: 'stop evidence', resolution: 'process tree stopped', document_version: 1, authorization_version: 1, actions: [] }] });
  await screen.findByText('处理：process tree stopped');
  expect(screen.getByLabelText('阻断历史')).toHaveProperty('value', 'history');
  expect(screen.queryByRole('button', { name: '提交决策处理' })).toBeNull();
  fireEvent.change(screen.getByLabelText('搜索阻断'), { target: { value: 'missing' } });
  expect(screen.queryByText('处理：process tree stopped')).toBeNull();
  expect(screen.getByText('没有匹配阻断')).toBeTruthy();
});
