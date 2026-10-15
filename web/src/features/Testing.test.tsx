import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import type { Snapshot, TestReadiness, TestSuite } from '../api.generated';
import { ApiClient } from '../api/client';
import { centerSnapshot } from '../test/connectionInfo';
import { localRun, localTask } from '../test/nodeSnapshot';
import { Testing } from './Testing';

const sha = 'a'.repeat(40);
const suite: TestSuite = { id: 'smoke', version: 3, name: '启动回归', description: '启动检查', stage: 'project', enabled: true, categories: ['黑盒', '回归', 'custom'], platforms: ['darwin'], requirement_ids: ['req_1'], match_paths: ['web/**'], output_paths: ['web/node_modules/**', 'web/*.tsbuildinfo'], block_merges_on_failure: true, checks: [{ name: 'check', command: 'npm', args: ['--prefix', 'web', 'test'], timeout_seconds: 60 }] };
const snapshot: Snapshot = { ...centerSnapshot, test_suites: [suite] };
const task = { ...localTask, id: 'verify', version: 8, title: '验证启动', tool: 'program', verification: { suite, commit: sha, digest: 'digest' } };
const report = { commit: sha, suite_id: suite.id, suite_version: 3, digest: 'digest', platform: 'darwin', checks: [{ name: 'check', passed: false, exit_code: 7, output: 'full bounded diagnostic\nsecond line', duration_seconds: 4 }] };
const run = { ...localRun, id: 'verification-run', task_id: task.id, kind: 'verification', tool: 'program', node_id: 'center', base: sha, state: 'failed', quiescent: true, test_report: report, detail: '检查失败摘要' };
const missing: TestReadiness = { commit: sha, ready: false, required: ['smoke@darwin'], passed: [], missing: ['smoke@darwin'], failed: [], uncovered_requirement_ids: ['req_1'] };
const passed: TestReadiness = { ...missing, ready: true, passed: ['smoke@darwin'], missing: [], uncovered_requirement_ids: [] };
function setup(state = snapshot, platform: 'darwin' | 'linux' | 'windows' | undefined = 'darwin') {
  const fetcher = vi.fn().mockResolvedValue(new Response('{}', { status: 200 }));
  vi.stubGlobal('fetch', fetcher);
  const client = new ApiClient('owner-token');
  const refresh = vi.fn();
  const view = render(<Testing snapshot={state} client={client} refresh={refresh} centerPlatform={platform} initialCommit={sha} />);
  return { fetcher, client, refresh, ...view };
}
function response(fetcher: ReturnType<typeof vi.fn>, body: unknown, status = 200) { fetcher.mockImplementationOnce(async () => new Response(JSON.stringify(body), { status })); }
function body(fetcher: ReturnType<typeof vi.fn>, index = 0) { return JSON.parse(fetcher.mock.calls[index][1].body); }
function tab(name: string) { fireEvent.click(screen.getByRole('button', { name })); }
function chooseSuite() { tab('套件'); fireEvent.click(screen.getByRole('button', { name: '执行 启动回归' })); }
function actionButton(name: string) { tab(name === '查询就绪状态' ? '覆盖与就绪' : ['暂停验证', '恢复验证', '取消验证'].includes(name) ? '执行记录' : '套件'); return screen.getByRole('button', { name }); }
function edit() { fireEvent.click(screen.getByRole('button', { name: '编辑 启动回归' })); }
function save() { fireEvent.click(screen.getByRole('button', { name: '保存套件' })); }
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks(); });

it('shows honest empty states and no green success without configuration', () => {
  setup({ ...centerSnapshot, test_suites: [] });
  expect(screen.getByText('尚未配置测试套件。没有配置不代表通过。')).toBeTruthy();
  expect(screen.getByText('尚无验证执行记录。')).toBeTruthy();
  expect(screen.queryByText('当前提交的所需测试已通过')).toBeNull();
  expect(screen.queryByRole('button', { name: '创建验证任务' })).toBeNull();
});
it('does not offer writes without a project', () => {
  const { fetcher } = setup({ ...snapshot, project: null });
  expect(screen.getByText('项目尚未初始化，暂不能配置或启动测试。')).toBeTruthy();
  expect(screen.queryByRole('button')).toBeNull();
  expect(fetcher).not.toHaveBeenCalled();
});
it('round trips output directories, cross categories, block semantics and exact Owner commands with expected_version', async () => {
  const { fetcher, refresh } = setup();
  edit();
  expect(screen.getByLabelText(/测试输出目录（每行一个）/)).toHaveProperty('value', 'web/node_modules/**\nweb/*.tsbuildinfo');
  fireEvent.change(screen.getByLabelText(/测试输出目录（每行一个）/), { target: { value: 'web/node_modules/**\nweb/*.tsbuildinfo\nreports/**' } });
  fireEvent.click(screen.getByLabelText('白盒'));
  save();
  await waitFor(() => expect(refresh).toHaveBeenCalled());
  expect(fetcher.mock.calls[0][0]).toBe('/api/v1/tests/suites');
  expect(fetcher.mock.calls[0][1]).toMatchObject({ method: 'POST', headers: { Authorization: 'Bearer owner-token', 'Content-Type': 'application/json' } });
  expect(body(fetcher)).toEqual({ expected_version: 3, suite: { ...suite, categories: ['黑盒', '白盒', '回归', 'custom'], output_paths: [...suite.output_paths!, 'reports/**'] } });
});
it('creates a new version-zero suite only with explicit Owner check configuration', async () => {
  const { fetcher } = setup();
  fireEvent.click(screen.getByRole('button', { name: '新增套件' }));
  fireEvent.change(screen.getByLabelText('套件 ID'), { target: { value: 'new_suite' } });
  fireEvent.change(screen.getByLabelText('套件名称'), { target: { value: '新套件' } });
  save();
  expect(await screen.findByRole('alert')).toHaveProperty('textContent', '检查命令必须是包含至少一项检查的 JSON 数组；请保留 Owner 确认的命令。');
  expect(fetcher).not.toHaveBeenCalled();
  fireEvent.change(screen.getByLabelText(/检查命令 JSON/), { target: { value: JSON.stringify(suite.checks) } });
  save();
  await waitFor(() => expect(fetcher).toHaveBeenCalled());
  expect(body(fetcher)).toMatchObject({ expected_version: 0, suite: { id: 'new_suite', version: 0, checks: suite.checks, stage: 'project' } });
});
it.each([[409, 'stale_version', '套件版本已变化，请刷新'], [403, 'forbidden', '仅 Owner 可保存'], [500, 'failed', '磁盘写入失败']])('preserves draft and exposes server rejection %s', async (status, code, message) => {
  const { fetcher, refresh } = setup(); response(fetcher, { code, message }, status);
  edit(); fireEvent.change(screen.getByLabelText('套件名称'), { target: { value: '保留编辑' } }); save();
  expect(await screen.findByRole('alert')).toHaveProperty('textContent', message);
  expect(screen.getByLabelText('套件名称')).toHaveProperty('value', '保留编辑');
  expect(body(fetcher).expected_version).toBe(3);
  expect(refresh).toHaveBeenCalled();
});
it('requires pause and stopped execution for merge edits, including transitions out of merge', () => {
  const merge = { ...suite, stage: 'merge' };
  const { rerender, client, refresh, fetcher } = setup({ ...snapshot, test_suites: [merge] });
  edit(); expect(screen.getByRole('button', { name: '保存套件' })).toHaveProperty('disabled', true);
  fireEvent.change(screen.getByLabelText('阶段 stage'), { target: { value: 'project' } });
  expect(screen.getByRole('button', { name: '保存套件' })).toHaveProperty('disabled', true);
  rerender(<Testing snapshot={{ ...snapshot, test_suites: [merge], project: { ...snapshot.project!, paused: true }, runs: [{ ...run, quiescent: false }] }} client={client} refresh={refresh} />);
  expect(screen.getByRole('button', { name: '保存套件' })).toHaveProperty('disabled', true);
  rerender(<Testing snapshot={{ ...snapshot, test_suites: [merge], project: { ...snapshot.project!, paused: true }, runs: [run] }} client={client} refresh={refresh} />);
  expect(screen.getByRole('button', { name: '保存套件' })).toHaveProperty('disabled', false);
  expect(fetcher).not.toHaveBeenCalled();
});
it('shows server merge rejection despite locally stopped evidence', async () => {
  const { fetcher } = setup({ ...snapshot, project: { ...snapshot.project!, paused: true }, test_suites: [{ ...suite, stage: 'merge' }] });
  response(fetcher, { code: 'busy', message: 'publication must be reconciled before changing merge policy' }, 409);
  edit(); save(); expect(await screen.findByRole('alert')).toHaveProperty('textContent', 'publication must be reconciled before changing merge policy');
});
it('posts pinned SHA, suite version, finite limits and requirement IDs; paused projects queue', async () => {
  const { fetcher } = setup({ ...snapshot, project: { ...snapshot.project!, paused: true } });
  chooseSuite();
  fireEvent.change(screen.getByLabelText('累计预算（秒）'), { target: { value: 900 } });
  fireEvent.change(screen.getByLabelText('单次执行上限（秒）'), { target: { value: 400 } });
  fireEvent.change(screen.getByLabelText('重试上限'), { target: { value: 2 } });
  fireEvent.change(screen.getByLabelText('执行关联需求（每行一个 ID）'), { target: { value: 'req_1\nreq_2' } });
  fireEvent.click(actionButton('加入验证队列'));
  await screen.findByText('验证任务已排队；项目暂停期间不会启动。');
  expect(fetcher.mock.calls[0][0]).toBe('/api/v1/tests/run');
  expect(body(fetcher)).toEqual({ suite_id: suite.id, expected_version: 3, commit: sha, budget_seconds: 900, max_run_seconds: 400, max_retries: 2, requirement_ids: ['req_1', 'req_2'] });
});
it('rejects a single-run limit larger than the cumulative budget with actionable feedback', async () => {
  const { fetcher } = setup(); chooseSuite();
  fireEvent.change(screen.getByLabelText('累计预算（秒）'), { target: { value: 20 } });
  fireEvent.click(actionButton('创建验证任务'));
  expect(await screen.findByRole('alert')).toHaveProperty('textContent', '请填写有限整数预算、单次上限不大于累计预算，重试范围为 0..2。');
  expect(fetcher).not.toHaveBeenCalled();
});
it.each(['linux', 'windows', undefined] as const)('blocks incompatible or unknown center platform %s', (platform) => {
  const { rerender, client, refresh, fetcher } = setup();
  rerender(<Testing snapshot={snapshot} client={client} refresh={refresh} centerPlatform={platform} initialCommit={sha} />);
  chooseSuite(); expect(actionButton('创建验证任务')).toHaveProperty('disabled', true);
  expect(screen.getByText(/与已确认中心平台不匹配或中心平台未知/)).toBeTruthy();
  expect(fetcher).not.toHaveBeenCalled();
});
it('allows center-local unrestricted suites but disables disabled suites and incomplete commits', () => {
  const { rerender, client, refresh } = setup({ ...snapshot, test_suites: [{ ...suite, platforms: [] }] });
  chooseSuite(); expect(actionButton('创建验证任务')).toHaveProperty('disabled', false);
  fireEvent.change(screen.getByLabelText('完整提交 SHA'), { target: { value: 'main' } });
  expect(actionButton('创建验证任务')).toHaveProperty('disabled', true);
  expect(actionButton('查询就绪状态')).toHaveProperty('disabled', true);
  fireEvent.change(screen.getByLabelText('完整提交 SHA'), { target: { value: sha } });
  rerender(<Testing snapshot={{ ...snapshot, test_suites: [{ ...suite, enabled: false }] }} client={client} refresh={refresh} centerPlatform="darwin" />);
  expect(actionButton('创建验证任务')).toHaveProperty('disabled', true);
});
it('surfaces run admission failures instead of claiming execution', async () => {
  const { fetcher } = setup(); response(fetcher, { code: 'conflict', message: 'suite disabled or unavailable on this center platform' }, 409);
  chooseSuite(); fireEvent.click(actionButton('创建验证任务'));
  expect(await screen.findByRole('alert')).toHaveProperty('textContent', 'suite disabled or unavailable on this center platform');
  expect(screen.queryByText('验证任务已创建，等待中心调度。')).toBeNull();
});
it('queries exact SHA and renders every readiness dimension without inferring pass from history', async () => {
  const { fetcher } = setup({ ...snapshot, runs: [{ ...run, state: 'succeeded', test_report: { ...report, commit: 'b'.repeat(40), suite_version: 2, platform: 'linux' } }] });
  response(fetcher, { ...missing, failed: ['release@windows'] });
  fireEvent.click(actionButton('查询就绪状态'));
  await screen.findByText('尚未就绪');
  expect(fetcher.mock.calls[0][0]).toBe(`/api/v1/tests/readiness?commit=${sha}`);
  const region = screen.getByRole('region', { name: '问题与覆盖' });
  for (const label of ['必需套件', '已通过', '缺失 / 未完成', '失败', '未覆盖需求']) expect(within(region).getByText(label)).toBeTruthy();
  expect(within(region).getByText('历史套件')).toBeTruthy();
  expect(within(region).getByText('关联需求')).toBeTruthy();
  expect(screen.queryByText('当前提交的所需测试已通过')).toBeNull();
});
it.each([
  { ...missing, ready: true, required: [], missing: [], uncovered_requirement_ids: [] },
  { ...passed, failed: ['smoke@darwin'] },
  { ...passed, missing: ['smoke@darwin'] },
  { ...passed, passed: [] },
  { ...passed, commit: 'b'.repeat(40) },
])('never greens incomplete or mismatched readiness %#', async (value) => {
  const { fetcher } = setup(); response(fetcher, value);
  fireEvent.click(actionButton('查询就绪状态'));
  await waitFor(() => expect(actionButton('查询就绪状态')).toHaveProperty('disabled', false));
  expect(screen.queryByText('当前提交的所需测试已通过')).toBeNull();
});
it('invalidates successful readiness when suite version or SHA changes', async () => {
  const { fetcher, client, refresh, rerender } = setup(); response(fetcher, passed);
  fireEvent.click(actionButton('查询就绪状态'));
  await screen.findByText('当前提交的所需测试已通过');
  rerender(<Testing snapshot={{ ...snapshot, test_suites: [{ ...suite, version: 4 }] }} client={client} refresh={refresh} />);
  expect(screen.queryByText('当前提交的所需测试已通过')).toBeNull();
  response(fetcher, passed); fireEvent.click(actionButton('查询就绪状态')); await screen.findByText('当前提交的所需测试已通过');
  fireEvent.change(screen.getByLabelText('完整提交 SHA'), { target: { value: 'b'.repeat(40) } });
  expect(screen.queryByText('当前提交的所需测试已通过')).toBeNull();
});
it('ignores late readiness for a different commit and shows network errors', async () => {
  const { fetcher } = setup();
  let resolve!: (value: Response) => void;
  fetcher.mockImplementationOnce(() => new Promise<Response>((done) => { resolve = done; }));
  fireEvent.click(actionButton('查询就绪状态'));
  fireEvent.change(screen.getByLabelText('完整提交 SHA'), { target: { value: 'b'.repeat(40) } });
  resolve(new Response(JSON.stringify(passed)));
  await waitFor(() => expect(actionButton('查询就绪状态')).toHaveProperty('disabled', false));
  expect(screen.queryByText('当前提交的所需测试已通过')).toBeNull();
  fetcher.mockRejectedValueOnce(new TypeError('offline'));
  fireEvent.click(actionButton('查询就绪状态'));
  expect((await screen.findByRole('alert')).textContent).toContain('无法连接中心');
});
it('preserves historical failures, full bounded output and fixed report identity; no merge action', () => {
  setup({ ...snapshot, runs: [run], tasks: [{ ...task, state: 'verified' }] });
  expect(screen.getByText('检查失败摘要')).toBeTruthy();
  expect(screen.getByText(/退出码 7/)).toBeTruthy();
  expect(screen.getByText('套件版本：v3 · 平台：macOS')).toBeTruthy();
  const details = screen.getByText('展开完整有界输出').closest('details')!;
  expect(details.open).toBe(false);
  fireEvent.click(within(details).getByText('展开完整有界输出'));
  expect(details.querySelector('pre')!.textContent).toBe(report.checks[0].output);
  expect(screen.queryByRole('button', { name: /合并|发布|恢复验证/ })).toBeNull();
  expect(screen.queryByText('当前提交的所需测试已通过')).toBeNull();
});
it('uses existing versioned pause/resume/cancel and project controls', async () => {
  const { fetcher, client, refresh, rerender } = setup({ ...snapshot, tasks: [task] });
  vi.spyOn(window, 'confirm').mockReturnValue(true);
  fireEvent.click(actionButton('暂停验证'));
  await waitFor(() => expect(refresh).toHaveBeenCalledTimes(1));
  expect(fetcher.mock.calls[0][0]).toBe('/api/v1/tasks/verify/pause'); expect(body(fetcher)).toEqual({ expected_version: 8 });
  rerender(<Testing snapshot={{ ...snapshot, tasks: [{ ...task, hold_reason: 'owner', version: 9 }] }} client={client} refresh={refresh} />);
  fireEvent.click(actionButton('恢复验证'));
  await waitFor(() => expect(refresh).toHaveBeenCalledTimes(2));
  expect(fetcher.mock.calls[1][0]).toBe('/api/v1/tasks/verify/resume'); expect(body(fetcher, 1)).toEqual({ expected_version: 9 });
  fireEvent.click(actionButton('取消验证'));
  await waitFor(() => expect(refresh).toHaveBeenCalledTimes(3));
  expect(fetcher.mock.calls[2][0]).toBe('/api/v1/tasks/verify/cancel');
  fireEvent.click(screen.getByRole('button', { name: '暂停项目' }));
  await waitFor(() => expect(refresh).toHaveBeenCalledTimes(4));
  expect(fetcher.mock.calls[3][0]).toBe('/api/v1/project/pause'); expect(body(fetcher, 3)).toEqual({ expected_version: snapshot.project!.version });
  rerender(<Testing snapshot={{ ...snapshot, project: { ...snapshot.project!, paused: true, version: 2 } }} client={client} refresh={refresh} />);
  fireEvent.click(screen.getByRole('button', { name: '核验并恢复项目' }));
  await waitFor(() => expect(refresh).toHaveBeenCalledTimes(5));
  expect(fetcher.mock.calls[4][0]).toBe('/api/v1/project/resume'); expect(body(fetcher, 4)).toEqual({ expected_version: 2 });
});

it.each(['cancelled', 'running', 'stopped', 'skipped'])('does not count %s or incomplete reports as a successful verification', async (state) => {
  const { fetcher } = setup({ ...snapshot, runs: [{ ...run, state, test_report: { ...report, checks: [] } }] });
  response(fetcher, missing);
  fireEvent.click(actionButton('查询就绪状态'));
  await screen.findByText('尚未就绪');
  expect(screen.getByText('未记录检查结果，不算通过。')).toBeTruthy();
  expect(screen.queryByText('当前提交的所需测试已通过')).toBeNull();
});
it('invalidates readiness when task acceptance changes and requires a fresh query', async () => {
  const { fetcher, client, refresh, rerender } = setup({ ...snapshot, tasks: [{ ...task, state: 'verified' }] });
  response(fetcher, passed);
  fireEvent.click(actionButton('查询就绪状态'));
  await screen.findByText('当前提交的所需测试已通过');
  rerender(<Testing snapshot={{ ...snapshot, tasks: [{ ...task, state: 'cancelled' }] }} client={client} refresh={refresh} />);
  expect(screen.queryByText('当前提交的所需测试已通过')).toBeNull();
});
it('blocks invalid retry counts before any write and supports the zero retry default', async () => {
  const { fetcher } = setup(); chooseSuite();
  fireEvent.change(screen.getByLabelText('重试上限'), { target: { value: 3 } });
  fireEvent.submit(actionButton('创建验证任务').closest('form')!);
  expect(fetcher).not.toHaveBeenCalled();
  fireEvent.change(screen.getByLabelText('重试上限'), { target: { value: 0 } });
  fireEvent.click(actionButton('创建验证任务'));
  await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1));
  expect(body(fetcher).max_retries).toBe(0);
});

it('honors server ready with uncovered requirements as a separate warning', async () => {
  const { fetcher } = setup(); response(fetcher, { ...passed, uncovered_requirement_ids: ['req_uncovered'] });
  fireEvent.click(actionButton('查询就绪状态'));
  await screen.findByText('当前提交的所需测试已通过');
  expect(within(screen.getByRole('region', { name: '问题与覆盖' })).getByText('关联需求')).toBeTruthy();
  expect(screen.getByText(/测试就绪不代表完整行为覆盖/)).toBeTruthy();
});
it('preserves a fixed query across development versions and all ordinary heartbeats', async () => {
  const state = { ...snapshot, tasks: [localTask, task], runs: [localRun, run] };
  const { fetcher, client, refresh, rerender } = setup(state); response(fetcher, passed);
  fireEvent.click(actionButton('查询就绪状态'));
  await screen.findByText('当前提交的所需测试已通过');
  rerender(<Testing snapshot={{ ...state, tasks: [{ ...localTask, version: 12, used_seconds: 48 }, { ...task, version: 17, used_seconds: 40 }], runs: [{ ...localRun, version: 19, lease_until: 'later', elapsed_seconds: 65 }, { ...run, version: 29, elapsed_seconds: 100, lease_until: 'later' }] }} client={client} refresh={refresh} />);
  expect(screen.getByText('当前提交的所需测试已通过')).toBeTruthy();
  expect(fetcher).toHaveBeenCalledTimes(1);
});
it.each(['suite-spec', 'generation', 'verification-state', 'report', 'requirements', 'task-links'])(
  'invalidates old readiness on relevant %s changes', async (change) => {
    const state: Snapshot = { ...snapshot, tasks: [task], runs: [run], requirements: [{ id: 'req_1', version: 1, title: '需求', body: '原文', created_at: '' }] };
    const { fetcher, client, refresh, rerender } = setup(state); response(fetcher, passed);
    fireEvent.click(actionButton('查询就绪状态'));
    await screen.findByText('当前提交的所需测试已通过');
    const updated: Snapshot = { ...state };
    if (change === 'suite-spec') updated.test_suites = [{ ...suite, checks: [{ ...suite.checks![0], timeout_seconds: 59 }] }];
    if (change === 'generation') updated.tasks = [{ ...task, generation: task.generation + 1 }];
    if (change === 'verification-state') updated.tasks = [{ ...task, state: 'cancelled' }];
    if (change === 'report') updated.runs = [{ ...run, test_report: { ...report, checks: [{ ...report.checks[0], passed: true, exit_code: 0 }] } }];
    if (change === 'requirements') updated.requirements = [...state.requirements!, { ...state.requirements![0], id: 'new_req' }];
    if (change === 'task-links') updated.tasks = [{ ...task, requirement_ids: ['different_requirement'] }];
    rerender(<Testing snapshot={updated} client={client} refresh={refresh} />);
    expect(screen.queryByText('当前提交的所需测试已通过')).toBeNull();
    // Returning to previous input cannot resurrect an invalidated result.
    rerender(<Testing snapshot={state} client={client} refresh={refresh} />);
    expect(screen.queryByText('当前提交的所需测试已通过')).toBeNull();
  },
);
it.each([40, 64])('normalizes a nonzero %s-digit OID for query and execution', async (length) => {
  const pinned = 'ab'.repeat(length / 2);
  const { fetcher } = setup();
  fireEvent.change(screen.getByLabelText('完整提交 SHA'), { target: { value: ` ${pinned.toUpperCase()} ` } });
  response(fetcher, { ...passed, commit: pinned });
  fireEvent.click(actionButton('查询就绪状态'));
  await screen.findByText('当前提交的所需测试已通过');
  expect(fetcher.mock.calls[0][0]).toBe(`/api/v1/tests/readiness?commit=${pinned}`);
  chooseSuite(); fireEvent.click(actionButton('创建验证任务'));
  await screen.findByText('验证任务已创建，等待中心调度。');
  expect(body(fetcher, 1).commit).toBe(pinned);
});
it.each(['0'.repeat(40), '0'.repeat(64), 'a'.repeat(39), 'a'.repeat(63), 'g'.repeat(64), 'main'])(
  'rejects invalid OID %s for both operations', (value) => {
    const { fetcher } = setup(); chooseSuite();
    fireEvent.change(screen.getByLabelText('完整提交 SHA'), { target: { value } });
    expect(actionButton('查询就绪状态')).toHaveProperty('disabled', true);
    expect(actionButton('创建验证任务')).toHaveProperty('disabled', true);
    expect(fetcher).not.toHaveBeenCalled();
  },
);
it('selects only real fixed candidate versions without claiming the current main', async () => {
  const fixed = { baseline_version: 1, baseline_digest: '', id: 'actual-candidate', version: 1, task_id: localTask.id, source_run_id: localRun.id, authorization_version: 1, policy_version: 1, base: sha, head: sha, main: sha, commit: 'b'.repeat(64), state: 'landed', detail: '', protected: false, approved_commit: '', checks: [], created_at: '' };
  const { fetcher } = setup({ ...snapshot, candidates: [fixed, { ...fixed, id: 'unfinished', commit: '' }] });
  tab('覆盖与就绪');
  const chooser = screen.getByLabelText('从真实候选选择固定版本');
  expect(within(chooser).getAllByRole('option')).toHaveLength(2);
  expect(screen.getByText(/不代表当前主线/)).toBeTruthy();
  fireEvent.change(chooser, { target: { value: fixed.commit } });
  expect(screen.getByLabelText('完整提交 SHA')).toHaveProperty('value', fixed.commit);
  response(fetcher, { ...passed, commit: fixed.commit });
  fireEvent.click(actionButton('查询就绪状态'));
  await screen.findByText('当前提交的所需测试已通过');
  expect(fetcher.mock.calls[0][0]).toBe(`/api/v1/tests/readiness?commit=${fixed.commit}`);
});
it('discards a pending query when verification evidence changes', async () => {
  const { fetcher, client, refresh, rerender } = setup();
  let resolve!: (response: Response) => void;
  fetcher.mockImplementationOnce(() => new Promise<Response>((done) => { resolve = done; }));
  fireEvent.click(actionButton('查询就绪状态'));
  rerender(<Testing snapshot={{ ...snapshot, tasks: [task] }} client={client} refresh={refresh} />);
  resolve(new Response(JSON.stringify(passed)));
  await waitFor(() => expect(actionButton('查询就绪状态')).toHaveProperty('disabled', false));
  expect(screen.queryByText('当前提交的所需测试已通过')).toBeNull();
});
it('preserves unknown Owner stage and platform values when explicitly saving a suite', async () => {
  const custom = { ...suite, stage: 'owner-stage', platforms: ['darwin', 'owner-platform'] };
  const { fetcher } = setup({ ...snapshot, test_suites: [custom] });
  expect(screen.getByText(/owner-stage · v3/)).toBeTruthy();
  edit(); save();
  await waitFor(() => expect(fetcher).toHaveBeenCalled());
  expect(body(fetcher).suite).toMatchObject({ stage: custom.stage, platforms: custom.platforms, name: custom.name, checks: custom.checks });
});
it('retains a report deep link to the actual failed run', () => {
  window.history.replaceState(null, '', `#testing?run=${run.id}`);
  const { container } = setup({ ...snapshot, runs: [run] });
  const report = screen.getByRole('article', { name: '测试报告 启动回归' });
  expect(report.id).toBe(`test-report-${run.id}`);
  expect(report.classList.contains('selected-record')).toBe(true);
  expect(container.querySelector('a[href="#testing?run=verification-run"]')).toBeTruthy();
  window.history.replaceState(null, '', '#');
});

it('shows suite meaning first and keeps exact Owner configuration behind technical details and unchanged on save', async () => {
  const original = { ...suite, categories: ['smoke', 'regression', 'white-box', 'black-box', 'e2e', 'golden-journey', 'long-running', 'Owner-special'] };
  const { fetcher } = setup({ ...snapshot, requirements: [{ id: 'req_1', version: 1, title: '真实需求标题', body: '原文', created_at: '' }], test_suites: [original] });
  expect(screen.getByText(/分类：冒烟、回归、白盒、黑盒、端到端、黄金旅程、长生命周期、Owner-special/)).toBeTruthy();
  expect(screen.getByRole('link', { name: '真实需求标题 →' }).getAttribute('href')).toBe('#requirements?id=req_1');
  const output = screen.getByText('测试输出目录：web/node_modules/**、web/*.tsbuildinfo');
  expect(output.closest('details')?.open).toBe(false);
  expect(screen.getByText('完整匹配规则：web/**').closest('details')).toBe(output.closest('details'));
  edit(); save();
  await waitFor(() => expect(fetcher).toHaveBeenCalled());
  expect(body(fetcher).suite).toEqual(original);
});
