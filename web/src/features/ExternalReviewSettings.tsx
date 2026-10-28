import './review-controls.css';
import type { ExternalReviewPolicy } from '../api.generated';

export function ExternalReviewSettings({ value, change }: { value?: ExternalReviewPolicy; change: (value?: ExternalReviewPolicy) => void }) {
  const kinds = value?.kinds ?? [];
  return <details className="review-controls"><summary>可信外部审查</summary>
    <label><input type="checkbox" checked={!!value} onChange={(event) => change(event.target.checked ? { provider: 'codex', actor_id: 0, app_id: 0, kinds: ['code_review', 'security_review'] } : undefined)} />要求 Codex 外部审查</label>
    {value && <>
      <label>机器人用户 ID<input type="number" min={1} step={1} required value={value.actor_id || ''} onChange={(event) => change({ ...value, actor_id: Number(event.target.value) })} /></label>
      <label>GitHub App ID<input type="number" min={1} step={1} required value={value.app_id || ''} onChange={(event) => change({ ...value, app_id: Number(event.target.value) })} /></label>
      {(['code_review', 'security_review'] as const).map((kind) => <label key={kind}><input type="checkbox" checked={kinds.includes(kind)} onChange={(event) => change({ ...value, kinds: event.target.checked ? [...kinds, kind] : kinds.filter((item) => item !== kind) })} />{kind === 'code_review' ? '代码审查' : '安全审查'}</label>)}
      {!kinds.length && <p role="alert">至少选择一类审查。</p>}
      <p className="muted">使用机器人结果的真实用户与 App ID。只认可当前提交的完整结果。</p>
    </>}
  </details>;
}
