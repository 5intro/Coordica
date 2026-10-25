import type { Candidate } from '../../api.generated';
import { Status, duration, short } from '../../components/ui';

type Props = { candidate: Candidate };

export function CandidateVersions({ candidate }: Props) {
  const versions = [
    ['B', candidate.base, '开发基线'], ['H', candidate.head, '成员提交'],
    ['M', candidate.main, '主线旧值'], ['C', candidate.commit, '最终候选'],
  ];
  return <details>
    <summary>技术详情：固定版本证据</summary>
    <dl className="commit-grid">{versions.map(([label, value, explanation]) => <div key={label}>
      <dt>{label}<span>{explanation}</span></dt><dd><code>{value || '尚未生成'}</code></dd>
    </div>)}</dl>
  </details>;
}

export function CandidateReview({ candidate }: Props) {
  const review = candidate.review;
  return <section className="detail-section">
    <h3>独立 Agent 评审</h3>
    {review ? <>
      <div className="subheading">
        {review.candidate === candidate.commit ? <Status value={review.verdict} />
          : <span>历史评审结论 {review.verdict} · 对象与当前 C 不同</span>}
        <a href={`#runs?id=${encodeURIComponent(review.run_id)}`}>查看评审运行</a>
      </div>
      <p className="preserve-lines">{review.summary}</p>
      {(review.findings ?? []).length > 0 && <ul className="findings">
        {(review.findings ?? []).map((finding, index) => <li key={index}>{finding}</li>)}
      </ul>}
      <details><summary>技术详情：评审对象</summary><code>{review.candidate}</code></details>
    </> : <p className="muted">尚无独立评审结论。</p>}
  </section>;
}

export function CandidateChecks({ candidate }: Props) {
  const checks = candidate.checks ?? [];
  return <section className="detail-section">
    <h3>程序门禁 <span className="count">{checks.length}</span></h3>
    <div className="muted">检查绑定 C：<code>{short(candidate.commit)}</code>
      {candidate.check_plan && <details>
        <summary>技术详情：检查计划</summary><p>政策 v{candidate.check_plan.policy_version}</p>
        <code>{candidate.check_plan.digest}</code>
      </details>}
      {candidate.check_run_id && <> · <a href={`#runs?id=${encodeURIComponent(candidate.check_run_id)}`}>查看检查运行</a></>}
    </div>
    {(candidate.check_plan?.checks ?? []).filter((check) => !checks.some((result) => result.name === check.name))
      .map((check) => <p key={check.name}>未执行 / 缺失：{check.name}</p>)}
    {checks.length ? checks.map((check, index) => <details className="check-result" key={`${check.name}-${index}`}>
      <summary>
        <span><span className={`check-dot ${check.passed ? 'passed' : 'failed'}`} aria-hidden="true" />{check.name}</span>
        <span className="muted">{check.passed ? '通过' : '失败'} · exit {check.exit_code} · {duration(check.duration_seconds)}</span>
      </summary>
      <pre>{check.output || '没有输出记录'}</pre>
    </details>) : <p className="muted">检查尚未执行；不会显示为通过。</p>}
  </section>;
}
