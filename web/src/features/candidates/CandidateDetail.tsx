import type { Candidate, Snapshot } from '../../api.generated';
import { ApiClient } from '../../api/client';
import { Feedback, Status, reasonLabel, short, time } from '../../components/ui';
import { useAction } from '../../hooks/useAction';
import { PullRequestStatus } from '../PullRequestStatus';
import { CandidateApproval } from './CandidateApproval';
import { CandidateChecks, CandidateReview, CandidateVersions } from './CandidateEvidence';
import { activeCandidate, candidateStageGuidance } from './candidateView';
import { ReviewIssues } from './ReviewIssues';

type Props = { candidate: Candidate; snapshot: Snapshot; client: ApiClient; refresh: () => void };

export function CandidateDetail({ candidate, snapshot, client, refresh }: Props) {
  const action = useAction(refresh);
  const approve = () => {
    if (window.confirm(`特别批准固定候选 ${candidate.commit}？仍需通过当前门禁，批准不等于运行版本启用。`)) {
      void action.execute(() => client.write<Candidate>(`/candidates/${encodeURIComponent(candidate.id)}/approve`, {
        expected_version: candidate.version,
      }), `已批准固定 C：${short(candidate.commit)}。`);
    }
  };
  return <article className="panel candidate-detail">
    <CandidateContext candidate={candidate} snapshot={snapshot} />
    {candidate.pull_request && <PullRequestStatus candidate={candidate} client={client} refresh={refresh} />}
    {candidate.detail && <p className="hold preserve-lines">{candidate.detail}</p>}
    <CandidateVersions candidate={candidate} />
    <CandidateApproval candidate={candidate} busy={action.busy} approve={approve} />
    <Feedback {...action} />
    <CandidateReview candidate={candidate} />
    <ReviewIssues key={`${candidate.id}:${candidate.commit}`} candidate={candidate} client={client} refresh={refresh} />
    <CandidateChecks candidate={candidate} />
    <p className="fine-print">普通候选由中心自动放行。已进入主线与运行版本启用分别记录。</p>
  </article>;
}

function CandidateContext({ candidate, snapshot }: Pick<Props, 'candidate' | 'snapshot'>) {
  const task = snapshot.tasks?.find((item) => item.id === candidate.task_id);
  return <>
    <div className="record-meta">
      <details><summary>技术详情</summary><code>{candidate.id}</code>
        <p>政策 v{candidate.policy_version} · 授权 v{candidate.authorization_version}</p>
      </details>
      <Status value={candidate.state} />
    </div>
    <h2>{task?.title || '未命名任务'}</h2>
    <a className="pool-detail-link" href={`#candidates?id=${encodeURIComponent(candidate.id)}`}>审查永久链接</a>
    <p>开发任务：<a href={`#tasks?id=${encodeURIComponent(candidate.task_id)}`}>{task?.title ?? '未命名任务'}</a></p>
    <div>需求：{task?.requirement_ids?.length ? task.requirement_ids.map((id) =>
      <a className="data-row" key={id} href={`#requirements?id=${encodeURIComponent(id)}`}>
        {snapshot.requirements?.find((item) => item.id === id)?.title ?? '关联需求'} →
      </a>) : '快照中未提供关联需求'}
    </div>
    {candidateStageGuidance[candidate.state] && <p>{candidateStageGuidance[candidate.state]}</p>}
    {activeCandidate(candidate.state) && task?.hold_reason && <p className="hold">
      任务受阻：{reasonLabel(task.hold_reason)}。请先查看开发任务中的原因与可用操作。
    </p>}
    <p className="muted">{time(candidate.created_at)}</p>
  </>;
}
