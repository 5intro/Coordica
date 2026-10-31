import type { Candidate } from '../api.generated';
import { ApiClient } from '../api/client';
import { Feedback, time } from '../components/ui';
import { useAction } from '../hooks/useAction';

const ciLabel = (value: string) => ({ success: '通过', failure: '失败', pending: '等待检查' }[value] ?? '尚未核对');
const reviewLabel = (value: string) => ({ approved: '已批准', changes_requested: '要求修改', none: '暂无评审记录', pending: '等待评审', commented: '已评论', dismissed: '已撤销' }[value] ?? value);
function safeGitHubURL(value: string) {
  try { const url = new URL(value); return url.protocol === 'https:' && url.hostname === 'github.com' && !url.username && !url.password ? url.href : undefined; } catch { return undefined; }
}

export function PullRequestStatus({ candidate, client, refresh }: { candidate: Candidate; client: ApiClient; refresh: () => void }) {
  const action = useAction(refresh);
  const pr = candidate.pull_request;
  if (!pr) return null;
  const current = pr.head === candidate.commit;
  const url = safeGitHubURL(pr.url);
  return <section className="detail-section" aria-label="GitHub 合并状态">
    <div className="subheading"><h3>{pr.number ? `PR #${pr.number}` : 'GitHub PR'}</h3>{url && <a href={url} target="_blank" rel="noreferrer">在 GitHub 查看</a>}</div>
    <dl className="facts"><dt>PR</dt><dd>{({ open: '进行中', closed: '已关闭', merged: '已合并' }[pr.state] ?? '等待同步')}{!current && ' · 历史版本'}</dd><dt>CI</dt><dd>{ciLabel(pr.ci)}</dd><dt>GitHub 评审</dt><dd>{reviewLabel(pr.review)}</dd><dt>同步时间</dt><dd>{time(pr.synced_at)}</dd></dl>
    {pr.detail && <p className="hold preserve-lines">{pr.detail}</p>}
    {pr.number > 0 && !['landed', 'invalidated', 'cancelled', 'rejected'].includes(candidate.state) && <button className="secondary" disabled={action.busy} onClick={() => void action.execute(() => client.write<Candidate>(`/candidates/${encodeURIComponent(candidate.id)}/sync`, { expected_version: candidate.version }), '已请求同步 PR。')}>同步 PR 状态</button>}
    <Feedback {...action} />
    {!!pr.external_reviews?.length && <details><summary>外部审查</summary>{pr.external_reviews.map((review) => <div className="settings-row" key={`${review.provider}/${review.kind}`}>
      <span>{review.kind === 'security_review' ? '安全审查' : '代码审查'}</span><span>{ciLabel(review.state)}{review.commit !== candidate.commit && ' · 历史版本'}</span>
      {safeGitHubURL(review.url) && <a href={safeGitHubURL(review.url)} target="_blank" rel="noreferrer">查看报告</a>}
    </div>)}</details>}
    {!!pr.checks?.length && <details><summary>CI 检查 · {pr.checks.length}</summary>{pr.checks.map((check, index) => <div className="settings-row" key={`${check.app_id}/${check.name}/${index}`}><span>{check.name}</span><span>{ciLabel(check.state)}</span></div>)}</details>}
    {!!pr.reviews?.length && <details><summary>GitHub 评审记录</summary>{pr.reviews.map((review) => <div className="settings-row" key={review.id}><span>{review.author}</span><span>{reviewLabel(review.state.toLowerCase())}{review.commit !== candidate.commit && ' · 历史版本'}</span></div>)}</details>}
  </section>;
}
