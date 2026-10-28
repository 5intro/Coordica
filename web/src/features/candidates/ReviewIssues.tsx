import { useEffect, useState } from 'react';
import '../review-controls.css';
import type { Candidate, ReviewIssue, ReviewIssuePage } from '../../api.generated';
import { ApiClient, messageOf } from '../../api/client';
import { Feedback, short } from '../../components/ui';
import { useAction } from '../../hooks/useAction';

const states: Record<string, string> = { open: '待处理', fixed: '已修复', false_positive: '误报', accepted_risk: '接受风险', superseded: '不再适用', resolved_external: '外部已处理' };

export function ReviewIssues({ candidate, client, refresh }: { candidate: Candidate; client: ApiClient; refresh: () => void }) {
  const [open, setOpen] = useState(false);
  const [page, setPage] = useState<ReviewIssuePage>();
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [revision, setRevision] = useState(0);
  const [after, setAfter] = useState('');
  useEffect(() => {
    setError('');
    if (!open) return;
    const abort = new AbortController();
    setLoading(true);
    client.read<ReviewIssuePage>(`/review-issues?task_id=${encodeURIComponent(candidate.task_id)}&limit=64&after=${encodeURIComponent(after)}`, abort.signal)
      .then((value) => { if (!abort.signal.aborted) setPage((old) => ({ ...value, items: after ? [...(old?.items ?? []).filter((item) => !(value.items ?? []).some((next) => next.id === item.id)), ...(value.items ?? [])] : value.items })); })
      .catch((failure) => { if (!abort.signal.aborted) setError(messageOf(failure)); })
      .finally(() => { if (!abort.signal.aborted) setLoading(false); });
    return () => abort.abort();
  }, [open, candidate.task_id, candidate.id, candidate.version, client, revision, after]);
  const reload = () => { setAfter(''); setRevision((value) => value + 1); refresh(); };
  return <section className="detail-section" aria-label="审查问题台账">
    <h3><button className="text-button" aria-expanded={open} onClick={() => setOpen(!open)}>审查问题</button></h3>
    {open && <>{loading && <p role="status">正在读取问题…</p>}{error && <p role="alert">{error}</p>}
      {page?.items?.map((issue) => <Issue key={`${issue.id}:${issue.version}:${candidate.commit}`} issue={issue} candidate={candidate} client={client} refresh={reload} />)}
      {page && !page.items?.length && <p className="muted">暂无问题记录。</p>}
      {page?.next && <button className="text-button" disabled={loading} onClick={() => setAfter(page.next)}>加载更多问题</button>}
      <button className="text-button" disabled={loading} onClick={reload}>刷新问题</button>
    </>}
    {candidate.review_rules && <details><summary>审查规则版本</summary>
      <p>固定主线 {short(candidate.review_rules.base)}</p>
      {!!candidate.review_rules.changed?.length && <p>包含规则变更，按原规则审查并单独批准。</p>}
      {candidate.review_rules.files?.map((file) => <details key={file.path}><summary>{file.path}</summary><pre>{file.text}</pre></details>)}
      <details><summary>技术详情</summary><code>{candidate.review_rules.digest}</code></details>
    </details>}
  </section>;
}

function Issue({ issue, candidate, client, refresh }: { issue: ReviewIssue; candidate: Candidate; client: ApiClient; refresh: () => void }) {
  const action = useAction(() => undefined);
  const [disposition, setDisposition] = useState('false_positive');
  const [reason, setReason] = useState('');
  const [recheck, setRecheck] = useState(false);
  const historical = issue.state !== 'open' && issue.resolution_commit !== candidate.commit;
  const external = issue.source.startsWith('external/');
  const editable = !external && !!candidate.commit && ['reviewing', 'rejected', 'validating', 'ready_to_land'].includes(candidate.state);
  return <div className="detail-section">
    <strong>{issue.finding.severity === 'unknown' ? '待分级' : issue.finding.severity} · {issue.finding.title}</strong>
    <p>{states[issue.state] ?? '待核对'}{historical && ' · 历史处置，待当前版本复核'}</p>
    <details><summary>查看依据</summary><p>{issue.finding.path}{issue.finding.line > 0 && `:${issue.finding.line}`}</p>
      <p className="preserve-lines">{issue.finding.trigger}</p><p className="preserve-lines">{issue.finding.evidence}</p><p>{issue.finding.impact}</p>
      {issue.resolution_reason && <p className="preserve-lines">处置依据：{issue.resolution_reason}</p>}
      <details><summary>技术详情</summary><code>{issue.id}</code><p>版本 {issue.version} · {issue.source}</p></details>
    </details>
    {external ? <p className="muted">在原审查服务处理，中心同步结果。</p> : editable && <details><summary>Owner 处置</summary>
      <form className="review-controls" onSubmit={(event) => { event.preventDefault(); void action.execute(() => client.write(`/review-issues/${encodeURIComponent(issue.id)}/resolve`, {
        expected_version: issue.version, candidate_id: candidate.id, commit: candidate.commit, disposition, reason, ...(recheck ? { request_recheck: true, candidate_version: candidate.version } : {}),
      }), '已记录处置；仍需通过当前候选的审查与门禁。').then((saved) => { if (saved) refresh(); }); }}>
        <label>处理方式<select aria-label={`处理方式：${issue.finding.title}`} value={disposition} onChange={(event) => setDisposition(event.target.value)}>
          <option value="false_positive">误报</option><option value="accepted_risk">接受风险</option><option value="superseded">不再适用</option>
        </select></label>
        <label>处置依据<textarea aria-label={`处置依据：${issue.finding.title}`} required maxLength={2048} value={reason} onChange={(event) => setReason(event.target.value)} /></label>
        {candidate.state === 'rejected' && <label><input type="checkbox" checked={recheck} onChange={(event) => setRecheck(event.target.checked)} />全部问题处置后重新审查此版本，保持暂停</label>}
        <button className="secondary" type="submit" disabled={action.busy || !reason.trim()}>记录处置</button><Feedback {...action} />
      </form>
    </details>}
  </div>;
}
