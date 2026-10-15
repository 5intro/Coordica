import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { App } from '../App';
import { LocalClient } from '../api/localClient';
import type { Candidate, Snapshot, TestSuite } from '../api.generated';
import { centerSnapshot, connectionInfo } from '../test/connectionInfo';
import { localRun, localTask, nodeSnapshot } from '../test/nodeSnapshot';
import { TOKEN_KEY } from './Login';

const sha = 'a'.repeat(40);
const task = { ...localTask, title: '黄金旅程任务', hold_reason: '等待人工核对' };
const candidate: Candidate = {
  id: 'journey-candidate', version: 1, task_id: task.id, source_run_id: localRun.id,
  authorization_version: 1, policy_version: 1, baseline_version: 1, baseline_digest: 'fixture',
  base: sha, head: sha, main: sha, commit: sha, state: 'prechecking', detail: '',
  protected: false, approved_commit: '', checks: [], created_at: '2026-10-11',
  check_plan: { policy_version: 1, digest: 'fixed-plan', checks: [{ name: '旅程检查', command: 'npm', args: ['test'], timeout_seconds: 60 }] },
};
const suite: TestSuite = { id: 'journey', version: 1, name: '黄金旅程检查', description: '', stage: 'project', enabled: true, categories: ['黄金旅程'], platforms: ['darwin'], requirement_ids: [], match_paths: ['web/**'], checks: [{ name: '旅程检查', command: 'npm', args: ['test'], timeout_seconds: 60 }] };
const snapshot: Snapshot = {
  ...centerSnapshot, tasks: [task], candidates: [candidate],
  requirements: [{ id: 'journey-requirement', version: 1, title: '黄金旅程需求', body: '用户原文证据', assistant_summary: '独立摘要证据', created_at: '2026-10-11' }],
};
// Exercise the shell and real ApiClient; only host discovery and HTTP are replaced.
function start(value = snapshot, route = '#overview') {
  window.history.replaceState(null, '', `/${route}`);
  sessionStorage.setItem(TOKEN_KEY, 'journey-owner');
  vi.spyOn(LocalClient.prototype, 'info').mockResolvedValue({ mode: 'center' });
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {});
  const request = vi.fn(async (url: string, _init?: RequestInit): Promise<Response> => {
    if (url === '/api/v1/state') return new Response(JSON.stringify(value));
    if (url === '/api/v1/system/connection') return new Response(JSON.stringify({ ...connectionInfo, platform: 'darwin' }));
    throw new Error(`Unexpected request: ${url}`);
  });
  vi.stubGlobal('fetch', request);
  render(<App />);
  return request;
}
function dock(name: string) {
  fireEvent.click(within(screen.getByRole('navigation', { name: '工作台页面' })).getByRole('button', { name }));
}
afterEach(() => {
  cleanup();
  sessionStorage.clear();
  window.history.replaceState(null, '', '/');
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

it.each([
  { page: '需求池', route: 'requirements', search: '搜索需求', query: '黄金', filter: '需求历史', value: 'active', item: '黄金旅程需求', link: '需求永久链接', id: 'journey-requirement' },
  { page: '任务池', route: 'tasks', search: '搜索任务', query: '黄金', filter: '任务筛选', value: 'active', item: task.title, link: '任务永久链接', id: task.id },
  { page: '审查池', route: 'candidates', search: '搜索审查', query: '黄金', filter: '审查筛选', value: 'all', item: /黄金旅程任务/, link: '审查永久链接', id: candidate.id },
  { page: '阻断池', route: 'blocks', search: '搜索阻塞', query: '人工', filter: '阻断历史', value: 'active', item: '任务 hold', link: '阻断永久链接', id: `task:${task.id}` },
])('从 Dock 浏览$page，筛选后进入详情并通过历史与 Dock 返回', async ({ page, route, search, query, filter, value, item, link, id }) => {
  start();
  await screen.findByRole('heading', { name: centerSnapshot.project!.name });
  dock(page);
  await screen.findByRole('heading', { name: page });
  fireEvent.change(screen.getByLabelText(search), { target: { value: query } });
  fireEvent.change(screen.getByLabelText(filter), { target: { value } });
  const listRoute = window.location.hash;
  fireEvent.click(screen.getByRole('button', { name: item }));
  const detailRoute = window.location.hash;
  expect(new URLSearchParams(detailRoute.split('?')[1]).get('id')).toBe(id);
  expect(screen.getByRole('link', { name: link }).getAttribute('href')).toBe(`#${route}?id=${encodeURIComponent(id)}`);
  window.history.back();
  await waitFor(() => expect(window.location.hash).toBe(listRoute));
  expect(screen.getByLabelText(search)).toHaveProperty('value', query);
  expect(screen.getByLabelText(filter)).toHaveProperty('value', value);
  window.history.forward();
  await waitFor(() => expect(window.location.hash).toBe(detailRoute));
  expect(screen.getByRole('link', { name: link })).toBeTruthy();
  dock('总览');
  await screen.findByRole('heading', { name: centerSnapshot.project!.name });
  dock(page);
  await screen.findByRole('heading', { name: page });
  expect(window.location.hash).toBe(detailRoute);
  expect(screen.getByLabelText(search)).toHaveProperty('value', query);
  expect(screen.getByLabelText(filter)).toHaveProperty('value', value);
});

it('从可复制需求深链接重开工作台，保留筛选和原文/摘要边界', async () => {
  start(snapshot, '#requirements?id=journey-requirement&query=黄金&history=active&sort=title');
  await screen.findByRole('heading', { name: '黄金旅程需求' });
  expect(screen.getByLabelText('需求排序')).toHaveProperty('value', 'title');
  expect(screen.getByLabelText('搜索需求')).toHaveProperty('value', '黄金');
  expect(screen.getByRole('heading', { name: '原始需求' })).toBeTruthy();
  expect(screen.getByText('用户原文证据')).toBeTruthy();
  expect(screen.getByRole('heading', { name: '助手摘要' })).toBeTruthy();
  expect(screen.getByText('独立摘要证据')).toBeTruthy();
});

it.each(['missing', 'skipped', 'running'] as const)('测试中心不把 %s 当作当前版本通过，审查也不把缺失检查当通过', async (state) => {
  const runs = state === 'missing' ? [] : [{ ...localRun, kind: 'verification', tool: 'program', node_id: 'center', state, test_report: { commit: sha, suite_id: suite.id, suite_version: 1, digest: 'fixed', platform: 'darwin', checks: [] } }];
  const request = start({ ...snapshot, test_suites: [suite], runs });
  await screen.findByRole('heading', { name: centerSnapshot.project!.name });
  dock('审查池');
  await screen.findByText('未执行 / 缺失：旅程检查');
  expect(screen.getByText('检查尚未执行；不会显示为通过。')).toBeTruthy();
  expect(screen.queryByText('评审通过')).toBeNull();
  dock('测试中心');
  await screen.findByRole('heading', { name: '测试中心' });
  expect(screen.getByText('尚无当前提交的有效就绪查询；未运行、跳过或取消均不算通过。')).toBeTruthy();
  if (state === 'missing') expect(screen.getByText('尚无验证执行记录。')).toBeTruthy();
  else expect(screen.getByText('未记录检查结果，不算通过。')).toBeTruthy();
  fireEvent.change(screen.getByLabelText('完整提交 SHA'), { target: { value: sha } });
  request.mockImplementationOnce(async () => new Response(JSON.stringify({ commit: sha, ready: false, required: ['journey@darwin'], passed: [], missing: ['journey@darwin'], failed: [], uncovered_requirement_ids: [] })));
  fireEvent.click(screen.getByRole('button', { name: '查询就绪状态' }));
  await screen.findByText('尚未就绪');
  expect(request).toHaveBeenCalledWith(`/api/v1/tests/readiness?commit=${sha}`, expect.anything());
  expect(screen.queryByText('当前提交的所需测试已通过')).toBeNull();
});

it('实际创建验证请求被拒绝后显示错误，保留输入且不宣称执行成功', async () => {
  const request = start({ ...snapshot, test_suites: [suite] });
  await screen.findByRole('heading', { name: centerSnapshot.project!.name });
  dock('测试中心');
  await screen.findByRole('heading', { name: '测试中心' });
  fireEvent.change(screen.getByLabelText('完整提交 SHA'), { target: { value: sha } });
  fireEvent.change(screen.getByLabelText('执行套件'), { target: { value: suite.id } });
  await waitFor(() => expect(screen.getByRole('button', { name: '创建验证任务' })).toHaveProperty('disabled', false));
  request.mockImplementationOnce(async () => new Response(JSON.stringify({ code: 'conflict', message: '套件版本已更新，请重新核对' }), { status: 409 }));
  fireEvent.click(screen.getByRole('button', { name: '创建验证任务' }));
  expect((await screen.findByRole('alert')).textContent).toBe('套件版本已更新，请重新核对');
  const call = request.mock.calls.find(([url]) => url === '/api/v1/tests/run')!;
  expect(JSON.parse(call[1]!.body as string)).toMatchObject({ suite_id: suite.id, expected_version: 1, commit: sha, max_retries: 0 });
  expect(screen.getByLabelText('完整提交 SHA')).toHaveProperty('value', sha);
  expect(screen.getByLabelText('执行套件')).toHaveProperty('value', suite.id);
  expect(screen.queryByText('验证任务已创建，等待中心调度。')).toBeNull();
});

it.each([false, true])('能力探测与真实活动实例分开呈现（有运行：%s）', async (running) => {
  start({ ...snapshot, nodes: [nodeSnapshot.node], runs: running ? [localRun, { ...localRun, id: 'lost-instance', state: 'lost', quiescent: false }] : [] });
  await screen.findByRole('heading', { name: centerSnapshot.project!.name });
  dock('Agent 管理');
  await screen.findByRole('heading', { name: 'Agent 管理' });
  const instances = screen.getByRole('heading', { name: '实际运行实例' }).closest('section')!;
  fireEvent.click(screen.getByText('工具能力与技术详情'));
  const capability = screen.getByRole('table');
  expect(within(capability).getByText('版本探测通过')).toBeTruthy();
  expect(within(capability).getByText('执行认证未提供')).toBeTruthy();
  expect(screen.getByText(/最后上报时间不代表在线/)).toBeTruthy();
  expect(screen.queryByRole('combobox', { name: /模型|思考/ })).toBeNull();
  if (!running) {
    expect(within(instances).getByText('暂无活动运行实例。已安装工具仅表示上报能力。')).toBeTruthy();
    expect(within(instances).queryByRole('link')).toBeNull();
  } else {
    expect(within(instances).getAllByRole('link').map((a) => a.getAttribute('href'))).toEqual([`#runs?id=${localRun.id}`, '#runs?id=lost-instance']);
    expect(within(instances).getByText('失联 · 未确认静止')).toBeTruthy();
    fireEvent.click(within(capability).getByRole('button', { name: /黄金旅程任务 · implement · running/ }));
    await screen.findByRole('heading', { name: '运行与节点' });
    expect(window.location.hash).toBe(`#runs?id=${localRun.id}`);
  }
});
