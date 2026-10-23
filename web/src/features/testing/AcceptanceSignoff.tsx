import { useEffect, useState } from 'react';
import type { AcceptanceCoverage, AcceptanceSignoffRequest, AcceptanceSignoffStatus } from '../../api.generated';
import { ApiClient, messageOf } from '../../api/client';
import { signoffStatus, submitSignoff } from '../../api/acceptanceSignoffs';
import { Feedback, Field } from '../../components/ui';
import { useAction } from '../../hooks/useAction';

type Props = { coverage: AcceptanceCoverage; client: ApiClient; refresh: () => void; onAccepted: (value: boolean) => void };

function useSignoff({ coverage, client, refresh, onAccepted }: Props) {
  const [view, setView] = useState<AcceptanceSignoffStatus>();
  const [error, setError] = useState('');
  const [revision, setRevision] = useState(0);
  const action = useAction(() => { setRevision((n) => n + 1); refresh(); });
  useEffect(() => {
    const controller = new AbortController();
    setView(undefined); setError(''); onAccepted(false);
    client.read<unknown>(`/tests/acceptance/signoff?requirement_id=${encodeURIComponent(coverage.requirement.id)}&commit=${coverage.commit}`,
      controller.signal).then((value) => {
      if (controller.signal.aborted) return;
      const next = signoffStatus(value, coverage);
      setView(next); onAccepted(next.accepted);
    }).catch((failure) => { if (!controller.signal.aborted) setError(messageOf(failure)); });
    return () => controller.abort();
  }, [client, coverage, onAccepted, revision]);
  async function save(body: AcceptanceSignoffRequest) {
    return action.execute(() => submitSignoff(client, coverage, body), '验收记录已保存。');
  }
  return { view, action, error, save, reload: () => setRevision((n) => n + 1) };
}

export function AcceptanceSignoffPanel(props: Props) {
  const { coverage, client } = props;
  const { view, action, error, save, reload } = useSignoff(props);
  const pending = client.acceptanceSignoffs.pending(coverage.requirement.id, coverage.commit);
  const [editing, setEditing] = useState(!!pending);
  const state = view?.accepted ? '已验收' : view?.receipt?.state === 'revoked' ? '已撤销验收'
    : view?.receipt ? '原验收已过期' : view ? '待人工验收' : error ? '验收记录未加载' : '正在读取验收…';
  const revoke = view?.receipt?.state === 'accepted';
  return <section className="panel signoff-panel" aria-label="人工验收">
    <div className="testing-heading"><h2>人工验收</h2><span role="status">{state}</span></div>
    {view?.receipt && <p className="signoff-note">{view.receipt.note}</p>}
    {error && <button className="secondary" onClick={reload}>重新读取</button>}
    {!editing && !pending && <button className="secondary" disabled={!view || !view.can_accept && !revoke}
      onClick={() => setEditing(true)}>{revoke ? '撤销验收' : '记录验收'}</button>}
    {view && !view.can_accept && !revoke && <small>先补齐当前版本的自动测试证据。</small>}
    {(editing || pending) && <SignoffForm key={pending?.key ?? `${view?.receipt?.version ?? 0}:${revoke}`}
      coverage={coverage} client={client} view={view} busy={action.busy} save={save} onDone={() => setEditing(false)} />}
    <Feedback error={action.error || error} notice={action.notice} />
    {!!view?.recent?.length && <details><summary>最近验收记录</summary><ol className="signoff-history">
      {view.recent?.map((r) => <li key={r.version}><span>{r.state === 'accepted' ? '签收' : '撤销'} · 需求修订 {r.revision} · 验收 v{r.plan_version} ·
        <time dateTime={r.recorded_at}>{new Date(r.recorded_at).toLocaleString()}</time></span><p>{r.note}</p></li>)}
    </ol><small>显示最近十次，完整历史保留在中心记录中。</small></details>}
  </section>;
}

type FormProps = Omit<Props, 'refresh' | 'onAccepted'> & { view?: AcceptanceSignoffStatus; busy: boolean;
  save: (body: AcceptanceSignoffRequest) => Promise<boolean>; onDone: () => void };

function SignoffForm({ coverage, client, view, busy, save, onDone }: FormProps) {
  const pending = client.acceptanceSignoffs.pending(coverage.requirement.id, coverage.commit);
  const [note, setNote] = useState(pending?.body.note ?? '');
  const [confirmed, setConfirmed] = useState<string[]>(pending?.body.manual_criteria ?? []);
  const revoke = pending ? pending.body.action === 'revoke' : view?.receipt?.state === 'accepted';
  const clauses = pending?.clauses ?? (coverage.criteria ?? []).filter((v) => v.criterion.method === 'manual').map((v) => v.criterion);
  const ready = pending || view && (revoke || view.can_accept) && !!note.trim() && (revoke || clauses.every((v) => confirmed.includes(v.id)));
  async function submit(event: React.FormEvent) {
    event.preventDefault();
    if (!ready || busy) return;
    const body: AcceptanceSignoffRequest = pending?.body ?? { requirement_id: coverage.requirement.id, commit: coverage.commit,
      action: revoke ? 'revoke' : 'accept', expected_version: view?.receipt?.version ?? 0,
      expected_digest: revoke ? '' : coverage.digest, manual_criteria: revoke ? [] : confirmed, note };
    if (await save(body)) onDone();
  }
  return <form className="signoff-form" onSubmit={(event) => void submit(event)} aria-label={revoke ? '撤销验收说明' : '记录人工验收'}>
    {pending && <p role="status">先确认原提交结果：需求修订 {pending.revision} · 验收 v{pending.planVersion}</p>}
    {!revoke && !!clauses.length && <fieldset disabled={busy || !!pending}><legend>确认人工条款</legend>
      {clauses.map((c) => <label className="checkbox" key={c.id}><input type="checkbox" checked={confirmed.includes(c.id)}
        onChange={(e) => setConfirmed((values) => e.target.checked ? [...values, c.id] : values.filter((id) => id !== c.id))} />{c.title}</label>)}
    </fieldset>}
    <Field label={revoke ? '撤销原因' : '验收说明'}><textarea required rows={3} maxLength={4096} value={note} disabled={busy || !!pending}
      onChange={(e) => setNote(e.target.value)} /></Field>
    <div className="signoff-actions"><button className="primary" disabled={busy || !ready}>
      {busy ? '正在保存…' : pending ? '确认原提交结果' : revoke ? '确认撤销' : '签收当前版本'}</button>
      {!pending && <button className="text-button" type="button" disabled={busy} onClick={onDone}>收起</button>}</div>
  </form>;
}
