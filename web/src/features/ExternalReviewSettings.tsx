import './review-controls.css';
import type { ExternalReviewPolicy } from '../api.generated';

export function ExternalReviewSettings({ value, change }: { value?: ExternalReviewPolicy; change: (value?: ExternalReviewPolicy) => void }) {
  const kinds = value?.kinds ?? [];
  return <details className="review-controls"><summary>可信外部审查</summary>
    <label><input type="checkbox" checked={!!value} onChange={(event) => change(event.target.checked ? { provider: 'codex', actor_id: 0, app_id: 0, kinds: ['code_review', 'security_review'] } : undefined)} />要求 Codex 外部审查</label>
    {value && <>
      <label>机器人用户 ID<input type="number" min={1} step={1} required value={value.actor_id || ''} onChange={(event) => change({ ...value, actor_id: Number(event.target.value) })} /></label>
      <label>GitHub App ID<input type="number" min={1} step={1} required value={value.app_id || ''} onChange={(event) => change({ ...value, app_id: Number(event.target.value) })} /></label>
      {(['code_review', 'security_review'] as const).map((kind) => <div key={kind}>
        <label><input type="checkbox" checked={kinds.includes(kind)} onChange={(event) => {
          const checkNames = { ...value.check_names };
          if (!event.target.checked) delete checkNames[kind];
          change({ ...value, check_names: checkNames, kinds: event.target.checked ? [...kinds, kind] : kinds.filter((item) => item !== kind) });
        }} />{kind === 'code_review' ? '代码审查' : '安全审查'}</label>
        {kinds.includes(kind) && <label>{kind === 'code_review' ? '代码审查检查名称' : '安全审查检查名称'}<input required maxLength={256} value={value.check_names?.[kind] ?? ''} placeholder="GitHub 中实际的 App 检查名称" onChange={(event) => change({ ...value, check_names: { ...value.check_names, [kind]: event.target.value } })} /></label>}
      </div>)}
      {!kinds.length && <p role="alert">至少选择一类审查。</p>}
      <p className="muted">检查须代表该类审查通过，并在分支保护中绑定此 App。仅有评论时保持等待。</p>
    </>}
  </details>;
}
