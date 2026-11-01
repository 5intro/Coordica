import { useEffect, useState } from 'react';
import type { WorkAnchor, WorkContext, WorkReference } from '../api.generated';
import { ApiClient } from '../api/client';
import { time, stateLabel } from '../components/ui';

function referenceLink(ref: WorkReference) {
  const pages: Record<string, string> = { requirement: 'requirements', task: 'tasks', run: 'runs', candidate: 'candidates', decision: 'blocks' };
  if (pages[ref.kind]) return `#${pages[ref.kind]}?id=${encodeURIComponent(ref.id)}`;
  if (ref.kind === 'initialization') return '#initialization';
  if (ref.kind === 'goal' || ref.kind === 'consultation') return '#planning';
  return undefined;
}

export function WorkHistory({ client, anchor }: { client: ApiClient; anchor: WorkAnchor }) {
  const [open, setOpen] = useState(false);
  const [value, setValue] = useState<WorkContext>();
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [busy, setBusy] = useState(false);
  const [query, setQuery] = useState({ cursor: '', reference_cursor: '', revision: 0 });
  const [, changed] = useState(0);
  const draft = client.workNotes.get(anchor);
  useEffect(() => {
    if (!open) return;
    const controller = new AbortController();
    setError('');
    void client.query<WorkContext>('/work/context', { anchor, limit: 20, cursor: query.cursor, reference_cursor: query.reference_cursor }, controller.signal)
      .then((next) => { if (!controller.signal.aborted) setValue(next); })
      .catch((e: unknown) => { if (!controller.signal.aborted) setError(e instanceof Error ? e.message : '记录读取失败，请刷新。'); });
    return () => controller.abort();
  }, [client, anchor.kind, anchor.id, open, query]);

  function refresh() { setQuery((q) => ({ cursor: '', reference_cursor: '', revision: q.revision + 1 })); }
  async function save() {
    setBusy(true); setError(''); setNotice('');
    try {
      await client.workNotes.save(client, anchor);
      setNotice('讨论已保存。'); refresh();
    } catch (e) { setError(e instanceof Error ? e.message : '保存未确认，请重试。'); }
    finally { setBusy(false); changed((n) => n + 1); }
  }

  const staleDraft = draft && value && draft.stamp !== value.current.reference.stamp;
  return <section className="detail-section work-history"><details onToggle={(e) => setOpen(e.currentTarget.open)}>
    <summary>工作记录</summary>
    {open && <>
      <p className="muted">保存讨论供后续接续，不会发送给施工 Agent 或修改需求与授权。</p>
      <div className="actions"><button type="button" onClick={refresh}>刷新当前事实</button>{value && <small>查询于 {time(value.queried_at)}</small>}</div>
      {error && <p role="alert">{error}</p>}{notice && <p role="status">{notice}</p>}
      {!value && !error && <p role="status">正在读取记录…</p>}
      {value && <>
        <p>{value.current.available
          ? `当前状态：${value.current.state === 'active' ? '有效' : stateLabel(value.current.state)}`
          : '当前详情无权读取；以下仅为已保存历史。'}</p>
        {value.current.available && value.current.stale && <p className="muted">关联版本已变化，请打开原记录核对。</p>}
        {(value.references ?? []).map((fact) => <details key={`${fact.reference.kind}:${fact.reference.id}`}>
          <summary>{fact.available ? fact.title || '关联记录' : '关联记录不可用'}{fact.stale && ' · 保存版本已过期'}</summary>
          {fact.available && <><p className="preserve-lines">{fact.text}</p>{fact.truncated && <p className="muted">此处为摘要，请打开原记录核对完整内容。</p>}
            {(fact.questions ?? []).map((question) => <p key={question.id} className="preserve-lines">{question.text} · {question.state === 'awaiting_answer' ? '待回答' : question.state === 'awaiting_owner' ? '待决定' : question.state === 'closed' ? '已结束' : '原记录待核对'}</p>)}
            {referenceLink(fact.reference) && <a href={referenceLink(fact.reference)}>打开原记录</a>}</>}
        </details>)}
        {value.next_reference_cursor && <button type="button" onClick={() => setQuery((q) => ({ ...q, reference_cursor: value.next_reference_cursor }))}>更多关联记录</button>}
        {value.notes?.length ? value.notes.map((note) => <div className="detail-section" key={note.id}>
          <small>{time(note.created_at)} · {note.chat_scope === 'owner' ? 'Owner' : '交互层'}{note.historical && ' · 历史版本'}</small>
          <p className="preserve-lines">{note.original_text || '已保存关联引用'}</p>
          {note.assistant_summary && <details><summary>助手摘要</summary><p className="preserve-lines">{note.assistant_summary}</p></details>}
        </div>) : <p className="muted">还没有保存讨论。</p>}
        {value.next_cursor && <button type="button" onClick={() => setQuery((q) => ({ ...q, cursor: value.next_cursor }))}>更早记录</button>}
        {value.current.available && <form className="form-stack" onSubmit={(e) => { e.preventDefault(); void save(); }}>
          <label>保存讨论<textarea aria-label="保存讨论" rows={3} maxLength={16384} value={draft?.text ?? ''} disabled={busy || !!draft?.request}
            onChange={(e) => { client.workNotes.edit(anchor, e.target.value, value.current.reference.stamp); changed((n) => n + 1); }} /></label>
          {staleDraft && !draft?.request && <p>当前事实已变化。<button type="button" onClick={() => { client.workNotes.adopt(anchor, value.current.reference.stamp); changed((n) => n + 1); }}>核对后采用当前版本</button></p>}
          {draft?.request && <p className="muted">保存结果待确认，重试会沿用原文、版本与请求身份。</p>}
          <button type="submit" className="secondary align-end" disabled={busy || !draft?.text.trim() || !!staleDraft && !draft?.request}>{busy ? '保存中…' : draft?.request ? '重试确认保存' : '保存讨论'}</button>
        </form>}
      </>}
    </>}
  </details></section>;
}
