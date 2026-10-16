import { useState, type FormEvent } from 'react';
import type { ApiClient } from '../api/client';
import type { Requirement } from '../api.generated';
import { useAction } from '../hooks/useAction';
import { Feedback, Field } from '../components/ui';

export function RequirementComposer({ client, refresh, onSaved }: { client: ApiClient; refresh: () => void; onSaved: () => void }) {
  const [title, setTitle] = useState('');
  const [body, setBody] = useState('');
  const action = useAction(refresh);
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!title.trim() || !body.trim()) return;
    if (await action.execute(() => client.write<Requirement>('/requirements', { title: title.trim(), body: body.trim() }), '需求已记录。')) onSaved();
  }
  return <form className="form-stack requirement-composer" onSubmit={submit}><Feedback {...action}/><Field label="需求标题"><input value={title} onChange={(e) => setTitle(e.target.value)} required/></Field><Field label="原始需求与成功条件"><textarea rows={6} value={body} onChange={(e) => setBody(e.target.value)} required placeholder="想解决什么问题？什么结果代表完成？"/></Field><button className="primary align-end" disabled={action.busy || !title.trim() || !body.trim()}>保存需求</button></form>;
}
