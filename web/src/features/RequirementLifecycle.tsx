import { useEffect, useState, type FormEvent } from 'react';
import type { Requirement, RequirementDetail } from '../api.generated';
import { ApiClient, messageOf } from '../api/client';
import { Feedback, Field, Status, time } from '../components/ui';
import { useAction } from '../hooks/useAction';

const changeLabels: Record<string, string> = { queued: '等待 Leader 规划', waiting_stop: '等待旧执行停止', assessing: 'Leader 评估中', applied: '已更新计划', blocked: '需要处理', superseded: '已被后续版本取代' };
export function RequirementLifecycle({ requirement, client, refresh }: { requirement: Requirement; client: ApiClient; refresh: () => void }) {
  const [mode, setMode] = useState<'revise' | 'withdraw' | null>(null);
  const [draft, setDraft] = useState({ version: requirement.version, title: requirement.title, body: requirement.body, reason: '' });
  const [detail, setDetail] = useState<RequirementDetail>();
  const [history, setHistory] = useState(false);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState('');
  const [reload, setReload] = useState(0);
  const action = useAction(() => { refresh(); setReload((value) => value + 1); });
  useEffect(() => {
    if (!mode && !history) return;
    const controller = new AbortController();
    setLoading(true); setLoadError('');
    client.read<RequirementDetail>(`/requirements/${encodeURIComponent(requirement.id)}`, controller.signal).then(setDetail).catch((error) => {
      if (!controller.signal.aborted) setLoadError(messageOf(error));
    }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [client, requirement.id, requirement.version, mode, history, reload]);
  const currentDetail = detail?.requirement.version === requirement.version ? detail : undefined;
  const latestChange = currentDetail?.changes?.find((change) => change.content_revision === (requirement.content_revision ?? 1));
  function begin(next: 'revise' | 'withdraw') {
    setDraft({ version: requirement.version, title: requirement.title, body: requirement.body, reason: '' });
    setMode(next);
  }
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!mode || !draft.reason.trim()) return;
    const body = { expected_version: draft.version, reason: draft.reason.trim(), ...(mode === 'revise' ? { title: draft.title.trim(), body: draft.body } : {}) };
    if (await action.execute(() => client.write(`/requirements/${encodeURIComponent(requirement.id)}/${mode}`, body), mode === 'revise' ? '修订已保存，等待重新规划。' : '需求已撤回，相关工作按影响处理。')) setMode(null);
  }
  async function moreHistory() {
    if (!detail?.before || loading) return;
    setLoading(true); setLoadError('');
    try {
      const older = await client.read<RequirementDetail>(`/requirements/${encodeURIComponent(requirement.id)}?before=${detail.before}`);
      if (older.requirement.version !== detail.requirement.version) { setLoadError('需求有新版本，请重新打开历史。'); return; }
      setDetail({ ...detail, revisions: [...(detail.revisions ?? []), ...(older.revisions ?? [])], changes: [...(detail.changes ?? []), ...(older.changes ?? [])], before: older.before });
    } catch (error) { setLoadError(messageOf(error)); } finally { setLoading(false); }
  }
  return <section aria-label="需求变更" className="requirement-lifecycle">
    <div className="actions">
      {requirement.state !== 'withdrawn' ? <><button className="secondary" aria-pressed={mode === 'revise'} onClick={() => begin('revise')} disabled={action.busy}>修订需求</button><button className="secondary" aria-pressed={mode === 'withdraw'} onClick={() => begin('withdraw')} disabled={action.busy}>撤回需求</button></> : <span className="muted">已撤回 · 历史保留</span>}
      <button className="text-button" aria-expanded={history} onClick={() => setHistory(!history)}>{history ? '收起历史' : '版本历史'}</button>
    </div>
    <Feedback {...action} error={action.error || loadError}/>
    {mode && <form className="form-stack" aria-label={mode === 'revise' ? '修订需求表单' : '撤回需求表单'} onSubmit={submit}>
      <h3>{mode === 'revise' ? '修订需求' : '撤回需求'}</h3>
      {mode === 'revise' ? <><Field label="修订标题"><input required maxLength={512} value={draft.title} onChange={(event) => setDraft({ ...draft, title: event.target.value })}/></Field><Field label="修订内容与成功条件"><textarea required rows={6} maxLength={16384} value={draft.body} onChange={(event) => setDraft({ ...draft, body: event.target.value })}/></Field></> : <p className="muted">取消专属任务；共享任务重新规划。已合并代码通过补偿任务处理。</p>}
      <Field label={mode === 'revise' ? '修订原因' : '撤回原因'}><textarea required rows={2} maxLength={4096} value={draft.reason} onChange={(event) => setDraft({ ...draft, reason: event.target.value })}/></Field>
      {draft.version !== requirement.version && <p role="alert">需求已变化。请保留草稿，核对最新内容后重新打开表单。</p>}
      <details open><summary>影响范围{currentDetail ? ` · ${(currentDetail.impact ?? []).length} 个任务` : ''}</summary>
        {loading && <p role="status">正在核对影响…</p>}
        {currentDetail?.impact?.map((impact) => <p className="data-row" key={impact.task.id}><span>{impact.task.title}<small>{impact.integrated ? '已完成 · 需要补偿评估' : impact.shared ? '共享任务 · 保留其他需求' : impact.needs_stop ? '在途 · 先停止再规划' : '未完成 · 重新规划'}</small></span><Status value={impact.task.state}/></p>)}
        {currentDetail && !(currentDetail.impact ?? []).length && <p className="muted">尚未关联任务。</p>}
        {mode === 'revise' && <p className="muted">影响执行的修订会先停止旧 Run，确认停止后重新规划。</p>}
      </details>
      <div className="actions"><button type="submit" className={mode === 'withdraw' ? 'primary danger' : 'primary'} disabled={action.busy || loading || !currentDetail || !draft.reason.trim() || mode === 'revise' && (!draft.title.trim() || !draft.body.trim())}>{action.busy ? '正在保存…' : mode === 'revise' ? '保存修订' : '确认撤回'}</button><button className="secondary" type="button" disabled={action.busy} onClick={() => setMode(null)}>放弃草稿</button></div>
    </form>}
    {history && <div aria-label="需求版本历史">
      {loading && <p role="status">正在读取历史…</p>}
      {latestChange && <p className="muted">{changeLabels[latestChange.state] || latestChange.state}{latestChange.detail && ` · ${latestChange.detail}`}</p>}
      {latestChange?.state === 'blocked' && <button className="secondary" disabled={action.busy} onClick={() => action.execute(() => client.write(`/requirements/${encodeURIComponent(requirement.id)}/reassess`, { expected_version: latestChange.version }), '已重新排队，仍遵守项目暂停和自动化设置。')}>重新评估变更</button>}
      {currentDetail?.revisions?.map((revision) => <details key={revision.requirement.content_revision ?? 1}><summary>第 {revision.requirement.content_revision ?? 1} 版 · {revision.requirement.state === 'withdrawn' ? '撤回' : revision.requirement.title} · {time(revision.created_at)}</summary><p className="preserve-lines">{revision.requirement.body}</p>{revision.reason && <p className="muted">{revision.reason}</p>}</details>)}
      {!!currentDetail?.before && <button className="text-button" disabled={loading} onClick={moreHistory}>更早版本</button>}
    </div>}
  </section>;
}
