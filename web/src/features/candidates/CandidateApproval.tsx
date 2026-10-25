import type { Candidate } from '../../api.generated';
import { short } from '../../components/ui';

type Props = { candidate: Candidate; busy: boolean; approve: () => void };

export function CandidateApproval({ candidate, busy, approve }: Props) {
  if (!candidate.protected) return null;
  const canApprove = candidate.commit && candidate.approved_commit !== candidate.commit && candidate.state === 'ready_to_land';
  return <section className="protected-notice">
    <strong>包含受保护变化</strong>
    <p>人工批准绑定同一个 C。批准不会启用新政策或替换运行中的中心。</p>
    {canApprove ? <button className="primary" disabled={busy} onClick={approve}>特别批准此 C</button>
      : <p className="muted">{candidate.approved_commit === candidate.commit && candidate.commit
        ? `已批准 ${short(candidate.approved_commit)}` : '尚无可批准的有效固定候选'}</p>}
  </section>;
}
