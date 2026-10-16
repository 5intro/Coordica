import { useEffect, useState } from 'react';
import type { Snapshot } from '../api.generated';
import type { Page } from '../App';
import { duration, reasonLabel, roleLabel, Status, time } from '../components/ui';
import { DetailPanel } from '../components/material/DetailPanel';
import { Glass } from '../components/material/Glass';
import { blockRecords } from './management';

export type WorkTarget = { page: Page; id: string };
export function WorkItemPanel({ snapshot, target, onClose, open, back, navigate }: { snapshot: Snapshot; target: WorkTarget; onClose: () => void; open: (target: WorkTarget) => void; back?: () => void; navigate: (page: Page, id?: string) => void }) {
  const [tab, setTab] = useState('overview');
  useEffect(() => setTab('overview'), [target.page, target.id]);
  const requirement = target.page === 'requirements' ? snapshot.requirements?.find((r) => r.id === target.id) : undefined;
  const candidate = target.page === 'candidates' ? snapshot.candidates?.find((r) => r.id === target.id) : undefined;
  const run = target.page === 'runs' ? snapshot.runs?.find((r) => r.id === target.id) : undefined;
  const block = target.page === 'blocks' ? blockRecords(snapshot).find((r) => r.id === target.id || r.decision?.id === target.id) : undefined;
  const task = snapshot.tasks?.find((r) => r.id === (candidate?.task_id ?? run?.task_id ?? block?.task?.id ?? (target.page === 'tasks' ? target.id : '')));
  const exists = !!(requirement || candidate || run || block || target.page === 'tasks' && task);
  const title = exists ? requirement?.title || task?.title || (block ? reasonLabel(block.reason) : '') || '未命名工作' : '记录不在当前快照中';
  const related: { page: Page; id: string; title: string; state?: string }[] = [];
  const addTask = (id: string) => { const item = snapshot.tasks?.find((r) => r.id === id); if (item) related.push({ page: 'tasks', id, title: item.title, state: item.state }); };
  if (requirement) {
    snapshot.tasks?.filter((t) => t.requirement_ids?.includes(requirement.id)).forEach((t) => addTask(t.id));
    snapshot.requirements?.filter((r) => r.id !== requirement.id && (requirement.related_ids?.includes(r.id) || r.related_ids?.includes(requirement.id))).forEach((r) => related.push({ page: 'requirements', id: r.id, title: r.title }));
  } else if (task) {
    task.requirement_ids?.forEach((id) => { const item = snapshot.requirements?.find((r) => r.id === id); if (item) related.push({ page: 'requirements', id, title: item.title }); });
    if (target.page !== 'tasks') addTask(task.id);
    else { task.dependencies?.forEach(addTask); if (task.current_candidate_id) { const c = snapshot.candidates?.find((r) => r.id === task.current_candidate_id); if (c) related.push({ page: 'candidates', id: c.id, title: '当前审查候选', state: c.state }); } }
    snapshot.runs?.filter((r) => r.task_id === task.id && r.id !== run?.id).slice(-3).reverse().forEach((r) => related.push({ page: 'runs', id: r.id, title: `${r.tool} · ${roleLabel(r.kind)}`, state: r.state }));
  }
  const objectIds = new Set([target.id, task?.id, run?.id, candidate?.id, block?.decision?.id]);
  const events = (snapshot.events ?? []).filter((e) => objectIds.has(e.object_id)).slice(-10).reverse();
  const label = ({ requirements: '需求池', tasks: '任务池', candidates: '审查池', blocks: '阻断池', runs: '运行记录' } as Partial<Record<Page, string>>)[target.page] ?? '工作详情';
  const status = run?.state ?? candidate?.state ?? (target.page === 'tasks' ? task?.state : undefined);
  const source = block?.decision ? { page: 'blocks' as Page, id: block.decision.id } : block ? { page: block.page, id: block.target } : target;
  return <DetailPanel title={title} onClose={onClose} back={back}>
    <p className="inspection-pool">{label}</p>{status && <Status value={status}/>}<h2>{title}</h2>
    {run && <p className="inspection-meta">{run.tool} · {snapshot.nodes?.find((n) => n.id === run.node_id)?.name || (run.node_id === 'center' ? '中心程序执行' : '主机未命名')} · {duration(run.elapsed_seconds)}</p>}
    {requirement && <p className="inspection-meta">{time(requirement.created_at)}</p>}
    {candidate?.commit && <p className="inspection-meta">固定版本 {candidate.commit.slice(0, 12)}</p>}
    {exists && <div className="inspection-tabs" role="tablist" aria-label="工作详情">{[['overview','概览'],['activity','动态'],['related',`关联 ${related.length}`]].map(([id,name]) => <button key={id} role="tab" aria-selected={tab === id} onClick={() => setTab(id)}>{name}</button>)}</div>}
    {!exists ? <p className="muted">记录可能已更新。可打开完整页面核对。</p> : tab === 'overview' ? <>
      {requirement && <><h3>原始需求</h3><p className="preserve-lines">{requirement.original_text || requirement.body}</p>{requirement.assistant_summary && <details><summary>助手摘要</summary><p className="preserve-lines">{requirement.assistant_summary}</p></details>}</>}
      {target.page === 'tasks' && task && <><p className="preserve-lines">{task.description}</p><h3>验收</h3><p className="preserve-lines">{task.acceptance || '尚未提供验收条件'}</p>{task.hold_reason && <p className="hold">{reasonLabel(task.hold_reason)}</p>}<details><summary>范围与执行限制</summary><p>{task.allowed_paths?.join('、') || '尚未提供范围'}</p><p>单次上限 {duration(task.max_run_seconds)} · 重试 {task.retries}/{task.max_retries}</p></details></>}
      {candidate && <><h3>独立审查</h3>{candidate.review?.candidate === candidate.commit ? <><Status value={candidate.review.verdict}/><p className="preserve-lines">{candidate.review.summary}</p></> : <p>尚无当前固定版本的独立评审结论。</p>}<h3>程序门禁</h3>{candidate.checks?.length ? candidate.checks.map((c,index) => <p key={index}>{c.name} · {c.passed && c.exit_code === 0 ? '通过' : '未通过'}</p>) : <p>尚无检查结果，不算通过。</p>}{candidate.protected && <p className="hold">包含受保护变化，请在审查页面核验固定版本。</p>}</>}
      {block && <><h3>{reasonLabel(block.reason)}</h3><p className="preserve-lines">{block.decision?.evidence || block.evidence || block.next}</p>{block.decision?.kind === 'scope_wait_stop' && <p className="hold">先核验原始运行的进程停止证明。</p>}</>}
      {run && <><p className="preserve-lines">{run.detail || '暂无补充运行记录。'}</p><p>{run.quiescent ? '进程已确认静止' : run.state === 'lost' ? '失联 · 未确认静止' : '进程尚未确认静止'}</p></>}
    </> : tab === 'activity' ? <>{events.length ? <ol className="inspection-events">{events.map((event) => <li key={event.sequence}><time>{time(event.time)}</time><p>{event.detail || event.kind}</p></li>)}</ol> : <p className="muted">当前快照暂无关联动态。</p>}<button className="text-button" onClick={() => navigate('audit')}>查看完整审计</button></> : <div className="inspection-related">{related.length ? related.map((r) => <Glass className="work-card" radius={14} key={`${r.page}:${r.id}`}><button onClick={() => { setTab('overview'); open(r); }}><strong>{r.title}</strong>{r.state && <Status value={r.state}/>}</button></Glass>) : <p className="muted">尚无关联工作。</p>}</div>}
    <button className="inspection-open secondary" onClick={() => navigate(source.page, source.id)}>打开{({ tasks:'任务', requirements:'需求', candidates:'审查', blocks:'阻断', runs:'运行' } as Partial<Record<Page,string>>)[source.page] ?? '完整'}页面 →</button>
  </DetailPanel>;
}
