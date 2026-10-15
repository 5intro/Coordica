import { reasonLabel, categoryLabel } from '../components/ui';
import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { MAX_RUN_DURATION_SECONDS, MAX_TASK_BUDGET_SECONDS } from '../api.generated';
import type { Snapshot, TestReadiness, TestSuite, TestSuiteSave, VerificationRequest } from '../api.generated';
import { ApiClient, messageOf } from '../api/client';
import { Feedback, Field, lines, time, stateLabel, platformLabel, stageLabel } from '../components/ui';
import { useAction } from '../hooks/useAction';
import './testing.css';
import { useRouteField } from '../hooks/useRouteField';

type Platform = 'darwin' | 'linux' | 'windows';
export interface TestingProps {
  snapshot: Snapshot;
  client: ApiClient;
  refresh: () => void;
  /** Trusted center OS supplied by the host integration; never the browser OS. */
  centerPlatform?: Platform;
  initialCommit?: string;
}
const categories = ['黑盒', '白盒', '冒烟', '回归', '黄金旅程', 'e2e', '长生命周期'];
const platforms: Platform[] = ['darwin', 'linux', 'windows'];
const normalizeCommit = (value: string) => value.trim().toLowerCase();
const fullCommit = (value: string) => /^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(normalizeCommit(value)) && !/^0+$/.test(normalizeCommit(value));
const newSuite = (): TestSuite => ({ id: '', version: 0, name: '', description: '', stage: 'project', enabled: true, categories: [], match_paths: [], output_paths: [], platforms: [], requirement_ids: [], checks: [], block_merges_on_failure: false });
const list = (values: string[] | null | undefined) => values?.join('、') || '无';

export function Testing({ snapshot, client, refresh, centerPlatform, initialCommit = '' }: TestingProps) {
  const action = useAction(refresh);
  const suites = snapshot.test_suites ?? [];
  const tasks = (snapshot.tasks ?? []).filter((task) => task.verification);
  const runs = (snapshot.runs ?? []).filter((run) => run.kind === 'verification').sort((a, b) => b.started_at.localeCompare(a.started_at));
  const [reportId] = useRouteField('testing', 'run');
  const [view, setView] = useRouteField('testing', 'view', reportId ? 'history' : 'suites');
  useEffect(() => { if (reportId) setView('history'); }, [reportId]);
  const suiteName = (id: string) => suites.find((item) => item.id === id.split('@')[0])?.name || '历史套件';
  const requirementNames = (ids: string[] | null | undefined) => list(ids?.map((id) => snapshot.requirements?.find((item) => item.id === id)?.title || '关联需求'));
  const suiteNames = (ids: string[] | null | undefined) => list(ids?.map(suiteName));
  const reportPresent = runs.some((run) => run.id === reportId);
  useEffect(() => {
    if (!reportId || !reportPresent) return;
    // Run after the shell restores page scroll for the same navigation.
    const frame = requestAnimationFrame(() => document.getElementById(`test-report-${reportId}`)?.scrollIntoView?.({ block: 'start' }));
    return () => cancelAnimationFrame(frame);
  }, [reportId, reportPresent, view]);
  const [draft, setDraft] = useState<TestSuite>();
  const [selected, setSelected] = useState('');
  const [commit, setCommit] = useState(initialCommit);
  const [runRequirements, setRunRequirements] = useState('');
  const [readiness, setReadiness] = useState<{ value: TestReadiness; key: string }>();
  const [reading, setReading] = useState(false);
  const [readError, setReadError] = useState('');
  const [runError, setRunError] = useState('');
  const readSequence = useRef(0);
  const fixedCommit = normalizeCommit(commit);
  // Ignore development heartbeats and accounting. Only test policy, verification
  // lifecycle/evidence and requirement associations invalidate fixed-SHA queries.
  const sorted = <T extends { id: string },>(items: T[]) => [...items].sort((a, b) => a.id.localeCompare(b.id));
  const evidenceKey = JSON.stringify([snapshot.project?.id, fixedCommit, sorted(suites),
    sorted(tasks).map((task) => [task.id, task.verification, task.state, task.generation, task.authorization_version, task.current_run_id, task.hold_reason, [...(task.requirement_ids ?? [])].sort()]),
    sorted(runs).map((run) => [run.id, run.task_id, run.state, run.generation, run.authorization_version, run.test_report]),
    sorted(snapshot.requirements ?? []).map((item) => [item.id, [...(item.related_ids ?? [])].sort()]),
    sorted((snapshot.tasks ?? []).filter((task) => task.requirement_ids?.length)).map((task) => [task.id, [...(task.requirement_ids ?? [])].sort()]),
  ]);
  useEffect(() => { ++readSequence.current; setReadiness(undefined); setReading(false); }, [evidenceKey]);
  const current = readiness?.key === evidenceKey && readiness.value.commit === fixedCommit ? readiness.value : undefined;
  const versions = (snapshot.candidates ?? []).filter((candidate) => fullCommit(candidate.commit));
  const suite = suites.find((item) => item.id === selected);
  const platformAllowed = Boolean(suite && (!(suite.platforms?.length) || (centerPlatform && suite.platforms.includes(centerPlatform))));
  const ready = Boolean(current?.ready && current.required?.length && !current.missing?.length && !current.failed?.length && current.required.every((id) => current.passed?.includes(id)));

  async function queryReadiness(event: FormEvent) {
    event.preventDefault();
    if (!fullCommit(commit)) { setReadError('请输入非零的完整 40 或 64 位提交 SHA。'); return; }
    const sequence = ++readSequence.current;
    const key = evidenceKey;
    setReading(true); setReadError(''); setReadiness(undefined);
    try {
      const value = await client.read<TestReadiness>(`/tests/readiness?commit=${encodeURIComponent(fixedCommit)}`);
      if (sequence === readSequence.current) setReadiness({ value, key });
    } catch (error) { if (sequence === readSequence.current) setReadError(messageOf(error)); }
    finally { if (sequence === readSequence.current) setReading(false); }
  }
  async function start(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setRunError('');
    if (!suite || !suite.enabled || !platformAllowed || !fullCommit(commit)) return;
    const data = new FormData(event.currentTarget);
    const request: VerificationRequest = { suite_id: suite.id, expected_version: suite.version, commit: fixedCommit, budget_seconds: Number(data.get('budget')), max_run_seconds: Number(data.get('maximum')), max_retries: Number(data.get('retries')), requirement_ids: lines(runRequirements) };
    if (!Number.isInteger(request.budget_seconds) || request.budget_seconds <= 0 || request.budget_seconds > MAX_TASK_BUDGET_SECONDS || !Number.isInteger(request.max_run_seconds) || request.max_run_seconds <= 0 || request.max_run_seconds > Math.min(request.budget_seconds, MAX_RUN_DURATION_SECONDS) || !Number.isInteger(request.max_retries) || request.max_retries < 0 || request.max_retries > 2) { setRunError('请填写有限整数预算、单次上限不大于累计预算，重试范围为 0..2。'); return; }
    await action.execute(() => client.write('/tests/run', request), snapshot.project?.paused ? '验证任务已排队；项目暂停期间不会启动。' : '验证任务已创建，等待中心调度。');
  }
  if (!snapshot.project) return <section className="testing-center"><h1>测试中心</h1><p>项目尚未初始化，暂不能配置或启动测试。</p></section>;
  return <div className="testing-center">
    <header className="testing-heading"><div><h1>测试中心</h1></div><div className="actions"><button type="button" className="secondary" onClick={refresh}>刷新测试状态</button><button className="secondary" disabled={action.busy} onClick={() => { const project = snapshot.project!; if (!project.paused && !window.confirm('暂停项目的新活动并请求执行停止？实际停止状态仍需核验。')) return; void action.execute(() => client.write(`/project/${project.paused ? 'resume' : 'pause'}`, { expected_version: project.version }), project.paused ? '已请求恢复项目。' : '已请求暂停项目，仍需确认执行停止。'); }}>{snapshot.project.paused ? '核验并恢复项目' : '暂停项目'}</button></div></header>
    <nav className="segmented" aria-label="测试视图">{[['suites', '套件'], ['history', '执行记录'], ['coverage', '覆盖与就绪']].map(([key, label]) => <button key={key} aria-pressed={view === key} onClick={() => setView(key)}>{label}</button>)}</nav>
    <Feedback {...action} />
    <section className="testing-section" hidden={view !== 'suites'} aria-labelledby="testing-suites"><div className="testing-heading"><h2 id="testing-suites">测试套件</h2><button className="secondary" onClick={() => setDraft(newSuite())}>新增套件</button></div>
      {!suites.length && <p>尚未配置测试套件。没有配置不代表通过。</p>}
      <div className="testing-columns" aria-hidden="true"><span>名称与配置</span><span>操作</span></div><ul className="testing-list suite-list">{suites.map((item) => <li key={item.id}><div><strong>{item.name}</strong> <span>{stageLabel(item.stage)} · v{item.version} · {item.enabled ? '启用' : '停用'}</span><p>分类：{list(item.categories?.map(categoryLabel))} · 平台：{item.platforms?.length ? item.platforms.map(platformLabel).join('、') : '中心本机'}</p>{item.block_merges_on_failure && <span className="testing-warning">失败阻断合并</span>}<div>关联需求：{item.requirement_ids?.length ? item.requirement_ids.map((id) => <a className="work-link" key={id} href={`#requirements?id=${encodeURIComponent(id)}`}>{snapshot.requirements?.find((requirement) => requirement.id === id)?.title ?? '关联需求'} →</a>) : '无'}</div><details><summary>技术详情</summary>{item.description && <p>{item.description}</p>}<p>套件 ID：<code>{item.id}</code> · v{item.version}</p><p>分类原始值：{list(item.categories)}</p><p>关联需求 ID：{list(item.requirement_ids)}</p><p>测试输出目录：{list(item.output_paths)}</p><p>完整匹配规则：{list(item.match_paths)}</p><pre>{JSON.stringify(item.checks, null, 2)}</pre></details></div><div className="actions"><button className="secondary" onClick={() => { setSelected(item.id); setRunRequirements((item.requirement_ids ?? []).join('\n')); }}>执行 {item.name}</button><button className="secondary" onClick={() => setDraft(structuredClone(item))}>编辑 {item.name}</button></div></li>)}</ul>
      {draft && <SuiteEditor key={`${draft.id}:${draft.version}`} original={draft} snapshot={snapshot} client={client} refresh={refresh} onClose={() => setDraft(undefined)} />}
    </section>
    <section className="testing-section" hidden={view !== 'coverage'} aria-labelledby="testing-coverage"><h2 id="testing-coverage">问题与覆盖</h2>
      <Field label="从真实候选选择固定版本"><select value="" onChange={(event) => { if (event.target.value) setCommit(event.target.value); }}><option value="">选择候选或已集成记录（不代表当前主线）</option>{versions.map((candidate) => <option key={candidate.id} value={normalizeCommit(candidate.commit)}>{snapshot.tasks?.find((task) => task.id === candidate.task_id)?.title ?? '未命名任务'} · {stateLabel(candidate.state)} · {candidate.commit.slice(0, 12)}</option>)}</select></Field><p>查询与执行使用下方同一个固定版本；可手动输入完整 SHA。</p>
      <form className="testing-query" onSubmit={queryReadiness}><Field label="完整提交 SHA"><input value={commit} onChange={(event) => { setCommit(event.target.value); setReadError(''); }} required maxLength={66} placeholder="非零 40 或 64 位固定提交" /></Field><button className="secondary" disabled={reading || !fullCommit(commit)}>查询就绪状态</button></form>
      <Feedback error={readError} />
      {!current ? <p>尚无当前提交的有效就绪查询；未运行、跳过或取消均不算通过。</p> : <div><p className={ready ? 'testing-pass' : 'testing-warning'} role="status">{ready ? '当前提交的所需测试已通过' : '尚未就绪'}</p><code>{current.commit.slice(0, 12)}</code><details><summary>技术详情</summary><pre>{JSON.stringify(current, null, 2)}</pre></details><dl className="testing-facts"><dt>必需套件</dt><dd>{suiteNames(current.required)}</dd><dt>已通过</dt><dd>{suiteNames(current.passed)}</dd><dt>缺失 / 未完成</dt><dd>{suiteNames(current.missing)}</dd><dt>失败</dt><dd>{suiteNames(current.failed)}</dd><dt>未覆盖需求</dt><dd>{requirementNames(current.uncovered_requirement_ids)}</dd></dl>{!!current.uncovered_requirement_ids?.length && <p className="testing-warning">需求关联覆盖提示：仍有未覆盖需求；测试就绪不代表完整行为覆盖。</p>}</div>}
      <p className="testing-note">仅查询明确提交的发布就绪状态，不自动发布。旧套件、其他提交或平台的历史结果不能替代当前证据。</p>
    </section>
    <section className="testing-section" hidden={view !== 'suites' || !selected} aria-labelledby="testing-start"><h2 id="testing-start">执行测试</h2><p>执行主机：中心 · {centerPlatform ? platformLabel(centerPlatform) : '中心平台尚未确认'}。当前不支持其他 Runner 执行检查命令。</p>
      {snapshot.project.paused && <p className="testing-warning">项目已暂停：新验证仅排队，恢复项目后才可调度。</p>}
      <Field label="执行固定提交 SHA"><input value={commit} onChange={(event) => setCommit(event.target.value)} placeholder="完整 40 或 64 位提交" /></Field><form className="testing-form" onSubmit={start}><Field label="执行套件"><select value={selected} onChange={(event) => { setSelected(event.target.value); setRunRequirements((suites.find((item) => item.id === event.target.value)?.requirement_ids ?? []).join('\n')); }} required><option value="">选择套件</option>{suites.map((item) => <option key={item.id} value={item.id}>{item.name} · v{item.version}</option>)}</select></Field>
        {suite && !platformAllowed && <p role="status">此套件限定 {list(suite.platforms)}，与已确认中心平台不匹配或中心平台未知，无法启动。</p>}
        {suite && !suite.enabled && <p>套件已停用，无法启动。</p>}
        <p>固定提交：<code>{fixedCommit.slice(0, 12) || '请先填写完整提交 SHA'}</code>{suite && ` · ${suite.name} v${suite.version}`}</p>
        <div className="testing-grid"><Field label="累计预算（秒）"><input name="budget" type="number" required min={1} max={MAX_TASK_BUDGET_SECONDS} defaultValue={600} /></Field><Field label="单次执行上限（秒）"><input name="maximum" type="number" required min={1} max={MAX_RUN_DURATION_SECONDS} defaultValue={300} /></Field><Field label="重试上限"><input name="retries" type="number" required min={0} max={2} defaultValue={0} /></Field></div>
        <fieldset><legend>关联需求</legend>{snapshot.requirements?.map((item) => <label className="checkbox" key={item.id}><input type="checkbox" checked={lines(runRequirements).includes(item.id)} onChange={(event) => setRunRequirements((event.target.checked ? [...lines(runRequirements), item.id] : lines(runRequirements).filter((id) => id !== item.id)).join('\n'))} />{item.title || '关联需求'}</label>)}<details><summary>技术详情：关联需求</summary><Field label="执行关联需求（每行一个 ID）"><textarea value={runRequirements} onChange={(event) => setRunRequirements(event.target.value)} rows={2} /></Field></details></fieldset>
        <Feedback error={runError} /><button className="primary" disabled={action.busy || !suite?.enabled || !platformAllowed || !fullCommit(commit)}>{snapshot.project.paused ? '加入验证队列' : '创建验证任务'}</button>
      </form>
    </section>
    <section className="testing-section" hidden={view !== 'history'} aria-labelledby="testing-history"><h2 id="testing-history">执行记录</h2>
      {!tasks.length && !runs.length && <p>尚无验证执行记录。</p>}
      <ul className="testing-list">{tasks.map((task) => <li key={task.id}><div><strong>{task.title}</strong><p>{stateLabel(task.state)} {task.hold_reason && `· 阻断原因：${reasonLabel(task.hold_reason)}`}</p><details><summary>技术详情</summary><p>任务 ID：<code>{task.id}</code></p><p>阻断原因原始值：<code>{task.hold_reason || '无'}</code></p></details><span>{task.verification!.commit.slice(0, 12)} · {suiteName(task.verification!.suite.id)} v{task.verification!.suite.version}</span><p>需求：{requirementNames(task.requirement_ids)}</p></div>{!['verified', 'cancelled', 'integrated'].includes(task.state) && <div className="actions"><button className="secondary" disabled={action.busy} onClick={() => void action.execute(() => client.write(`/tasks/${encodeURIComponent(task.id)}/${task.hold_reason ? 'resume' : 'pause'}`, { expected_version: task.version }), '已请求更新验证任务状态。')}>{task.hold_reason ? '恢复验证' : '暂停验证'}</button><button className="secondary" disabled={action.busy} onClick={() => { if (window.confirm('取消此验证任务的继续执行授权？历史报告会保留。')) void action.execute(() => client.write(`/tasks/${encodeURIComponent(task.id)}/cancel`, { expected_version: task.version }), '已请求取消验证任务。'); }}>取消验证</button></div>}</li>)}</ul>
      {runs.map((run) => <article className={`testing-run ${reportId === run.id ? 'selected-record' : ''}`} aria-label={`测试报告 ${suiteName(run.test_report?.suite_id ?? '')}`} id={`test-report-${run.id}`} key={run.id}><details open={reportId === run.id}><summary><strong>{suiteName(run.test_report?.suite_id ?? '')}</strong> · {stateLabel(run.state)} · {time(run.started_at)} · {run.test_report ? platformLabel(run.test_report.platform) : '平台未记录'}</summary><a href={`#testing?run=${encodeURIComponent(run.id)}`}>报告永久链接</a><p>{run.node_id === 'center' && run.tool === 'program' ? '中心程序执行' : `${snapshot.nodes?.find((node) => node.id === run.node_id)?.name || '主机未命名'} / ${run.tool}`} · {time(run.started_at)} · {run.quiescent ? '进程已静止' : '进程尚未确认静止'}</p>{run.detail && <p className="testing-summary">{run.detail}</p>}{run.test_report ? <><p>提交：<code>{run.test_report.commit.slice(0, 12)}</code></p><p>套件版本：v{run.test_report.suite_version} · 平台：{platformLabel(run.test_report.platform)}</p><details><summary>技术详情</summary><p>完整提交：<code>{run.test_report.commit}</code></p><p>套件：<code>{run.test_report.suite_id}</code></p><p>摘要指纹：<code>{run.test_report.digest}</code></p><code>{run.id} · {run.node_id} / {run.tool}</code></details>{!run.test_report.checks?.length && <p>未记录检查结果，不算通过。</p>}{run.test_report.checks?.map((check, index) => <div className="testing-check" key={`${check.name}:${index}`}><strong>{check.name}</strong><span> · {check.passed && check.exit_code === 0 ? '报告通过（历史）' : '未通过'} · 退出码 {check.exit_code} · {check.duration_seconds} 秒</span><details><summary>展开完整有界输出</summary><pre>{check.output || '没有输出。'}</pre></details></div>)}</> : <p>尚无测试报告，不算通过。</p>}</details></article>)}
      
    </section>
  </div>;
}

function SuiteEditor({ original, snapshot, client, refresh, onClose }: { original: TestSuite; snapshot: Snapshot; client: ApiClient; refresh: () => void; onClose: () => void }) {
  const action = useAction(refresh);
  const [stage, setStage] = useState(original.stage);
  const [validation, setValidation] = useState('');
  const merge = stage === 'merge' || original.stage === 'merge';
  const mergeBlocked = merge && (!snapshot.project?.paused || (snapshot.runs ?? []).some((run) => !run.quiescent) || (snapshot.planning_runs ?? []).some((run) => !run.tree_stopped));
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setValidation('');
    const data = new FormData(event.currentTarget);
    let checks: TestSuite['checks'];
    try { checks = JSON.parse(String(data.get('checks'))); if (!Array.isArray(checks) || !checks.length) throw new Error(); }
    catch { setValidation('检查命令必须是包含至少一项检查的 JSON 数组；请保留 Owner 确认的命令。'); return; }
    const selectedCategories = [...new Set([...data.getAll('category').map(String), ...lines(String(data.get('otherCategories')))])];
    const suite: TestSuite = { ...original, id: String(data.get('id')).trim(), name: String(data.get('name')).trim(), description: String(data.get('description')), stage, enabled: data.has('enabled'), categories: selectedCategories.length === (original.categories ?? []).length && selectedCategories.every((category) => original.categories?.includes(category)) ? [...original.categories!] : selectedCategories, platforms: data.getAll('platform').map(String), match_paths: lines(String(data.get('paths'))), output_paths: lines(String(data.get('outputs'))), requirement_ids: lines(String(data.get('requirements'))), checks, block_merges_on_failure: data.has('block') };
    const request: TestSuiteSave = { expected_version: original.version, suite };
    if (await action.execute(() => client.write('/tests/suites', request), '套件已保存为新版本。')) onClose();
  }
  return <form className="testing-editor testing-form" onSubmit={save}><div className="testing-heading"><h3>{original.version ? `编辑套件 · v${original.version}` : '新增测试套件'}</h3><button type="button" className="secondary" onClick={onClose}>收起编辑</button></div>
    <div className="testing-grid"><Field label="套件 ID"><input name="id" required readOnly={original.version > 0} defaultValue={original.id} /></Field><Field label="套件名称"><input name="name" required defaultValue={original.name} /></Field><Field label="阶段 stage"><select value={stage} onChange={(event) => setStage(event.target.value)}><option value="merge">合并检查</option><option value="project">项目验证</option><option value="release">发布回归</option>{!['merge', 'project', 'release'].includes(original.stage) && <option value={original.stage}>{original.stage}</option>}</select></Field></div>
    <Field label="套件说明"><textarea name="description" defaultValue={original.description} rows={2} /></Field>
    <div className="actions"><label><input name="enabled" type="checkbox" defaultChecked={original.enabled} /> 启用套件</label><label><input name="block" type="checkbox" defaultChecked={original.block_merges_on_failure} /> 失败阻断合并</label></div>
    <fieldset><legend>可交叉分类</legend><div className="actions">{categories.map((category) => <label key={category}><input name="category" type="checkbox" value={category} defaultChecked={original.categories?.includes(category)} /> {categoryLabel(category)}</label>)}</div><Field label="其他分类（每行一个）"><textarea name="otherCategories" rows={2} defaultValue={original.categories?.filter((category) => !categories.includes(category)).join('\n')} /></Field></fieldset>
    <fieldset><legend>执行平台（不选表示中心本机）</legend><div className="actions">{[...new Set([...platforms, ...(original.platforms ?? [])])].map((platform) => <label key={platform}><input name="platform" type="checkbox" value={platform} defaultChecked={original.platforms?.includes(platform)} /> {platformLabel(platform)}</label>)}</div></fieldset>
    <Field label="关联需求（每行一个 ID）"><textarea name="requirements" rows={2} defaultValue={original.requirement_ids?.join('\n')} /></Field>
    <Field label="匹配路径（每行一个）"><textarea name="paths" rows={2} defaultValue={original.match_paths?.join('\n')} /></Field>
    <Field label="测试输出目录（每行一个）" hint="Owner 声明的检查生成目录，例如 web/node_modules/**、web/*.tsbuildinfo。仅允许检查结束时留下这些未跟踪或忽略输出；受跟踪源码仍须完全一致。"><textarea name="outputs" rows={3} defaultValue={original.output_paths?.join('\n')} /></Field>
    <details><summary>高级配置：Owner 本机检查命令</summary><p>仅显式保存您的命令配置。不会根据需求自动生成或修改命令。</p><Field label="检查命令 JSON" hint="数组项包含 name、command、args、timeout_seconds；不通过 shell 拼接参数。"><textarea name="checks" rows={8} required defaultValue={JSON.stringify(original.checks ?? [], null, 2)} /></Field></details>
    {merge && <p className="testing-warning">merge 套件变更要求项目暂停且全部执行停止；中心仍会检查实际停止证据及候选状态。</p>}
    {mergeBlocked && <p role="status">请先暂停项目并确认全部执行已停止，再保存 merge 套件。</p>}
    <Feedback error={validation || action.error} notice={action.notice} />
    <button className="primary" disabled={action.busy || mergeBlocked}>保存套件</button>
  </form>;
}
