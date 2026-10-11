import { useState } from 'react';
import type { FormEvent } from 'react';
import type { Requirement } from '../api.generated';
import { ApiClient } from '../api/client';
import { Empty, Feedback, Field, SectionTitle, time } from '../components/ui';
import { useAction } from '../hooks/useAction';

export function Requirements({ requirements, client, refresh }: { requirements: Requirement[]; client: ApiClient; refresh: () => void }) {
  const [open, setOpen] = useState(false);
  const action = useAction(refresh);
  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const form = event.currentTarget; const data = new FormData(form);
    if (await action.execute(() => client.write<Requirement>('/requirements', { title: String(data.get('title')).trim(), body: String(data.get('body')).trim() }), '需求已记录。')) { form.reset(); setOpen(false); }
  }
  return <><SectionTitle eyebrow="02 / REQUIREMENTS" title="需求池" description="保存原始目标与依据，任务可以关联多个需求。" actions={<button className="primary" onClick={() => setOpen(!open)}>{open ? '收起表单' : '记录需求 ＋'}</button>} /><Feedback {...action} />{open && <form className="panel form-stack" onSubmit={create}><Field label="需求标题"><input name="title" required autoFocus /></Field><Field label="原始需求与成功条件"><textarea name="body" required rows={6} /></Field><button className="primary align-end" disabled={action.busy}>保存需求</button></form>}{requirements.length ? <div className="requirement-grid">{requirements.map((requirement) => <article className="panel" key={requirement.id}><div className="record-meta"><code>{requirement.id}</code><span>v{requirement.version}</span></div><h2>{requirement.title}</h2>{requirement.source_node_id && <p className="muted">节点提交 · <code>{requirement.source_node_id}</code></p>}<p className="preserve-lines">{requirement.body}</p><small className="muted">记录于 {time(requirement.created_at)}</small></article>)}</div> : <Empty title="需求池为空">先记录要解决的问题，再形成可执行任务。</Empty>}</>;
}
