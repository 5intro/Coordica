import { useState } from 'react';
import type { FormEvent } from 'react';
import type { Initialization, Snapshot } from '../api.generated';
import { ApiClient } from '../api/client';
import { Feedback, Field, SectionTitle } from '../components/ui';
import { useAction } from '../hooks/useAction';

const labels: Record<string, string> = { draft: '草案 · 尚未初始化', analyzing: '正在分析 / 访谈', interview: '待回答问题', awaiting_owner: '待 Owner 确认', initialized: '共享基线已确认' };
export function ProjectInitialization({ snapshot, client, refresh }: { snapshot: Snapshot; client: ApiClient; refresh: () => void }) {
  const v = snapshot.initialization;
  const project = snapshot.project;
  const action = useAction(refresh);
  const [kind, setKind] = useState('existing');
  const [reviewed, setReviewed] = useState('');
  const [answer, setAnswer] = useState('');
  const [question, setQuestion] = useState('');
  const [evidence, setEvidence] = useState('');
  const result = v?.result;
  const missingChecks = !project?.checks?.length;
  const canAnalyze = !!project?.paused && v?.status !== 'analyzing';
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!v) return;
    const ok = await action.execute(() => client.write<Initialization>('/initialization/answer', { expected_version: v.version, question_id: question, original_text: answer, source: { session_id: 'owner-console', message_id: crypto.randomUUID() } }), '原始反馈已保存。Leader 将重新判断当前 frontier。');
    if (ok) { setAnswer(''); setQuestion(''); setReviewed(''); }
  }
  return <><SectionTitle title="项目初始化" /><Feedback {...action} />
    <section className="panel" aria-labelledby="initialization-status"><h2 id="initialization-status">{labels[v?.status ?? 'draft']}</h2>
      <p role="status">{v?.status === 'initialized' ? '下一步：由 Owner 设置和批准目标；项目保持原有暂停与自动化设置。' : v?.status === 'awaiting_owner' ? '下一步：审阅下方草案、原始反馈与覆盖记录，确认当前版本和摘要。' : v?.status === 'interview' ? '下一步：回答尚未解决的问题。已回答的原文保留，下轮只讨论依赖已确定的决定。' : v?.status === 'analyzing' ? '等待有限只读请求的结果。未知存活需要 Owner 实际核验停止后恢复。' : '下一步：暂停项目，确认无活动执行、Leader 请求或待处理候选，然后开始分析。'}</p>
      {missingChecks && <p role="status">当前尚无有效检查：可先分析 / 访谈；Owner 必须在下方配置真实检查，再重新分析，才能确认共享基线。</p>}
      {v?.detail && <p className="muted">{v.detail}</p>}
      <dl className="facts"><dt>固定项目</dt><dd>{project?.name ?? '请先建立项目'}</dd><dt>输入分支</dt><dd>{project?.target_branch}</dd><dt>证据 SHA</dt><dd><code>{v?.source_sha || '新项目访谈尚无提交；首次开发前由 Owner 建立 Git 基础提交'}</code></dd><dt>草案版本</dt><dd>v{v?.version ?? 0}</dd><dt>已报告检查 / 可用文件</dt><dd>{v?.covered_paths?.length ?? 0} / {v?.available_paths?.length ?? 0}</dd><dt>共享基线证据</dt><dd><details><summary>技术详情</summary><code>{v?.digest || '等待有效结构化结果'}</code></details></dd></dl>
      <Field label="项目输入类型" hint="现有提交必须进行源码分析。新项目仅适用于明确选定的无提交仓库或新目录；服务端独立核验。"><select value={kind} onChange={(e) => setKind(e.target.value)}><option value="existing">已有仓库 · 固定提交只读分析</option><option value="new">新项目 · 依赖 frontier 访谈</option></select></Field>
      <button className="primary" disabled={action.busy || !canAnalyze} onClick={() => void action.execute(() => client.write<Initialization>('/initialization/analyze', { expected_version: v?.version ?? 0, kind }), '只读分析请求已登记。未授权任何开发任务。')}>{v?.status === 'initialized' ? '暂停后重新初始化（使旧任务授权失效）' : '打开项目并请求只读初始化'}</button>
      {v?.status === 'analyzing' && v.operation_state !== 'unknown_live' && <button className="secondary" disabled={action.busy} onClick={() => void action.execute(() => client.write('/initialization/cancel', { expected_version: v.version }), '已请求取消；实际停止仍需进程回执。')}>取消当前分析</button>}
      {v?.operation_state === 'unknown_live' && <form onSubmit={(e) => { e.preventDefault(); void action.execute(() => client.write('/initialization/confirm-stopped', { expected_version: v.operation_version, evidence }), '停止核验已登记。保留有限请求的累计支出，需重新分析。'); }}><Field label="实际停止检查记录" hint="由 Owner 在原主机检查该分析进程及所有子进程后填写；超时或模型声明不能作为证据。"><textarea required value={evidence} onChange={(e) => setEvidence(e.target.value)} /></Field><button className="secondary" disabled={action.busy || !evidence.trim()}>登记 Owner 停止核验</button></form>}
    </section>
    {project && <section className="panel"><details open={missingChecks}><summary>Owner 配置实际检查政策</summary><p>检查建议尚未生效。保存将使旧基线与任务授权失效；请保持暂停，等待当前分析实际停止后再保存并重新分析。</p><form onSubmit={(e) => { e.preventDefault(); const text = String(new FormData(e.currentTarget).get('checks')); void action.execute(() => client.write('/project/policy', { expected_version: project.version, checks: JSON.parse(text), protected_paths: project.protected_paths }), '真实检查政策已保存。请重新分析并确认精确基线。'); }}><Field label="实际检查 JSON" hint="非空数组；每项含 name、command、args（字符串数组）、timeout_seconds（1–3600）。不通过 shell 执行。"><textarea name="checks" rows={6} required defaultValue={JSON.stringify(project.checks ?? [], null, 2)} /></Field><button className="secondary" disabled={action.busy || !project.paused || v?.status === 'analyzing'}>保存 Owner 检查政策</button></form></details></section>}
    {result && <section className="panel"><h2>{result.initialization_version === v?.version ? '拟议共享上下文' : '上轮草案 · 新反馈分析中，不能确认'}</h2><p className="muted">事实引用与架构建议分别展示。检查与保护路径必须与当前 Owner 政策一致；建议不会修改有效政策。</p>{(['requirements', 'acceptance', 'architecture', 'modules', 'conventions', 'testing', 'functionality', 'risks'] as const).map((field, i) => <div className="detail-section" key={field}><h3>{['需求', '验收', '架构', '目录与模块责任', '代码与接口约定', '测试约定', '已知功能与未完成项', '风险'][i]}</h3><p style={{ whiteSpace: 'pre-wrap' }}>{result.baseline[field]}</p></div>)}<h3>允许目录</h3><p>{result.baseline.allowed_roots?.join('、')}</p><h3>有效保护路径</h3><p>{result.baseline.protected_paths?.join('、')}</p><h3>有效检查（分析期间不执行）</h3><ul>{result.baseline.checks?.map((check) => <li key={check.name}>{check.name} · {check.timeout_seconds} 秒</li>)}</ul><h3>源码事实与证据</h3><ul>{result.facts?.map((fact, i) => <li key={i}><code>{fact.path}</code>：{fact.fact}</li>)}</ul><h3>待 Owner 决定的建议</h3><ul>{result.suggestions?.map((suggestion, i) => <li key={i}>{suggestion}</li>)}</ul></section>}
    {(v?.status === 'interview' || v?.status === 'awaiting_owner') && <section className="panel"><h2>问题与原始反馈</h2>{result?.questions?.map((q) => <div className="detail-section" key={q.id}><h3>{q.text}</h3>{!!q.prerequisites?.length && <small>已确定前提：{q.prerequisites.join('、')}</small>}<button className="secondary" onClick={() => setQuestion(q.id)}>回答此问题</button></div>)}<form onSubmit={submit}><Field label="当前问题"><select value={question} onChange={(e) => setQuestion(e.target.value)}><option value="">补充原始反馈</option>{result?.questions?.map((q) => <option key={q.id} value={q.id}>{q.text}</option>)}</select></Field><Field label="原始回答 / 反馈"><textarea required maxLength={16384} value={answer} onChange={(e) => setAnswer(e.target.value)} rows={5} /></Field><button className="secondary" disabled={action.busy || !answer.trim()}>保存原文并继续访谈</button></form></section>}
    {!!v?.feedback?.length && <section className="panel"><h2>保留的原始回答</h2>{v.feedback.map((f) => <blockquote key={f.id}><p style={{ whiteSpace: 'pre-wrap' }}>{f.original_text}</p><details><summary>技术详情：反馈来源</summary><small>v{f.version} · {f.actor_id} · {f.question_id || '反馈'} · {f.source.session_id} / {f.source.message_id}</small></details></blockquote>)}</section>}
    {v && <section className="panel"><h2>覆盖与省略</h2><details><summary>已报告检查的固定源码文件</summary><ul>{v.covered_paths?.map((path) => <li key={path}><code>{path}</code></li>)}</ul></details><details><summary>省略项与原因</summary><ul>{v.omissions?.map((path, i) => <li key={i}>{path}</li>)}</ul></details><p className="muted">忽略文件、私有 docs、环境文件、凭据和运行数据不进入分析。相关源码超限或模块覆盖缺失不能完成初始化。</p></section>}
    {v?.status === 'awaiting_owner' && <section className="panel"><h2>Owner 确认精确基线</h2><label><input type="checkbox" checked={reviewed === `${v.version}:${v.digest}`} onChange={(e) => setReviewed(e.target.checked ? `${v.version}:${v.digest}` : '')} />我已审阅 v{v.version} 与摘要 {v.digest}，确认以上共享上下文及当前有效政策。</label><p>确认只批准共享基线，不批准目标、任务、范围、集成或发布，也不解除暂停。</p><button className="primary" disabled={missingChecks || action.busy || reviewed !== `${v.version}:${v.digest}`} onClick={() => void action.execute(() => client.write<Initialization>('/initialization/confirm', { expected_version: v.version, digest: v.digest }), '精确共享基线已确认。后续开发仍需独立授权。')}>确认已审阅的共享基线</button></section>}
  </>;
}
