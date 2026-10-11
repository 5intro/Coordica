import { useState } from 'react';
import type { FormEvent } from 'react';
import type { Requirement, Task } from '../api.generated';
import { ApiClient } from '../api/client';
import { Empty, Feedback, Field, SectionTitle, time } from '../components/ui';
import { matches } from './management';
import { useAction } from '../hooks/useAction';

export function Requirements({ requirements, client, refresh, tasks = [], navigate }: { tasks?: Task[]; navigate?: (page: 'tasks', id?: string) => void; requirements: Requirement[]; client: ApiClient; refresh: () => void }) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [source, setSource] = useState('all');
  const [selected, setSelected] = useState(requirements[0]?.id ?? '');
  const shown = requirements.filter((r) => (source === 'all' || (source === 'center' ? !r.source_node_id : r.source_node_id === source)) && matches(query, [r.id, r.title, r.body, r.source_node_id]));
  const requirement = requirements.find((r) => r.id === selected);
  const related = tasks.filter((t) => (t.requirement_ids ?? []).includes(selected));
  const action = useAction(refresh);
  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const form = event.currentTarget; const data = new FormData(form);
    if (await action.execute(() => client.write<Requirement>('/requirements', { title: String(data.get('title')).trim(), body: String(data.get('body')).trim() }), '需求已记录。')) { form.reset(); setOpen(false); }
  }
  return <><SectionTitle eyebrow="02 / REQUIREMENTS" title="需求池" description="保存原始目标与依据，任务可以关联多个需求。" actions={<button className="primary" onClick={() => setOpen(!open)}>{open ? '收起表单' : '记录需求 ＋'}</button>} /><Feedback {...action} />{open && <form className="panel form-stack" onSubmit={create}><Field label="需求标题"><input name="title" required autoFocus /></Field><Field label="原始需求与成功条件"><textarea name="body" required rows={6} /></Field><button className="primary align-end" disabled={action.busy}>保存需求</button></form>}<div className="task-toolbar"><input aria-label="搜索需求" type="search" placeholder="标题、内容、来源或 ID" value={query} onChange={(e) => setQuery(e.target.value)} /><select aria-label="需求来源" value={source} onChange={(e) => setSource(e.target.value)}><option value="all">全部来源 · {requirements.length}</option><option value="center">中心记录</option>{[...new Set(requirements.flatMap((r) => r.source_node_id ? [r.source_node_id] : []))].map((id) => <option key={id} value={id}>节点 {id}</option>)}</select></div><div className="management-layout"><section className="panel table-scroll">{shown.length ? <table><thead><tr><th>需求</th><th>来源</th><th>关联 / 已集成</th><th>创建时间</th></tr></thead><tbody>{shown.map((r) => { const linked = tasks.filter((t) => (t.requirement_ids ?? []).includes(r.id)); return <tr key={r.id} className={selected === r.id ? 'selected-record' : undefined}><td><button className="text-button" aria-pressed={selected === r.id} onClick={() => setSelected(r.id)}>{r.title}</button><small>{r.id} · v{r.version}</small></td><td>{r.source_node_id || '中心记录'}</td><td>{linked.length} / {linked.filter((t) => t.state === 'integrated').length}</td><td>{time(r.created_at)}</td></tr>; })}</tbody></table> : <Empty title={requirements.length ? '没有匹配需求' : '需求池为空'}>调整搜索与来源，或记录要解决的问题。</Empty>}</section><article className="panel">{requirement ? <><div className="record-meta"><code>{requirement.id}</code><span>v{requirement.version}</span></div><h2>{requirement.title}</h2><p className="muted">来源：{requirement.source_node_id || '中心记录'} · {time(requirement.created_at)}</p><p className="preserve-lines">{requirement.body}</p><h3>关联任务 · {related.length}（已集成 {related.filter((t) => t.state === 'integrated').length}）</h3>{related.length ? related.map((task) => <button key={task.id} className="data-row" onClick={() => navigate?.('tasks', task.id)}>{task.title} →</button>) : <p className="muted">尚未关联任务。</p>}</> : <Empty title="选择一项需求">查看原始内容与实际关联任务。</Empty>}</article></div></>;
}
