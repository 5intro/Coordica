import { useState } from 'react';
import type { Baseline, Initialization, Project } from '../../api.generated';
import type { PreparationControls } from './types';

const fields: [keyof Pick<Baseline,
  'requirements' | 'acceptance' | 'architecture' | 'modules' | 'conventions' | 'testing' | 'functionality' | 'risks'
>, string][] = [
  ['requirements', '需求'], ['acceptance', '验收'], ['architecture', '架构'], ['modules', '目录与模块责任'],
  ['conventions', '代码与接口约定'], ['testing', '测试约定'], ['functionality', '功能与未完成项'], ['risks', '风险'],
];

export function BaselineView({ current }: { current: Initialization }) {
  const confirmed = current.status === 'initialized' && current.approved_version === current.version
    && current.approved_digest === current.digest && !!current.approved_baseline;
  const baseline = confirmed ? current.approved_baseline : current.result?.baseline;
  if (!baseline) return null;
  const currentDraft = current.result?.initialization_version === current.version;
  const title = confirmed ? '已确认项目约定' : currentDraft ? '待确认草案' : '上轮草案';
  return (
    <section className="panel" aria-labelledby="baseline-heading">
      <h2 id="baseline-heading">{title}</h2>
      {!confirmed && <p className="muted">草案尚未生效。新的回答可继续完善约定，只有 Owner 能确认。</p>}
      {fields.map(([key, label]) => (
        <details className="detail-section" key={key} open={key === 'requirements'}>
          <summary>{label}</summary>
          <p className="preserve-lines">{baseline[key] || '待补充'}</p>
        </details>
      ))}
      <details className="detail-section">
        <summary>执行边界与检查</summary>
        <h3>允许目录</h3><p>{baseline.allowed_roots?.join('、')}</p>
        <h3>受保护路径</h3><p>{baseline.protected_paths?.join('、')}</p>
        <h3>有效检查</h3>
        <ul>{baseline.checks?.map((check) => <li key={check.name}>{check.name} · {check.timeout_seconds} 秒</li>)}</ul>
      </details>
    </section>
  );
}

export function InitializationConfirmation({ current, project, client, action }: PreparationControls & {
  current: Initialization; project: Project;
}) {
  const [reviewed, setReviewed] = useState('');
  if (current.status !== 'awaiting_owner') return null;
  const identity = `${current.version}:${current.digest}`;
  return (
    <section className="panel">
      <h2>确认项目约定</h2>
      <label>
        <input type="checkbox" checked={reviewed === identity}
          onChange={(event) => setReviewed(event.target.checked ? identity : '')} />
        我已审阅当前草案与原始回答，确认以上项目约定和检查政策。
      </label>
      <p className="muted">确认后项目仍保持暂停，开发目标与集成另行授权。</p>
      <button className="primary" disabled={!project.checks?.length || action.busy || reviewed !== identity}
        onClick={() => void action.execute(() => client.write<Initialization>('/initialization/confirm', {
          expected_version: current.version, digest: current.digest,
        }), '精确共享基线已确认。后续开发仍需独立授权。')}>确认已审阅的共享基线</button>
    </section>
  );
}
