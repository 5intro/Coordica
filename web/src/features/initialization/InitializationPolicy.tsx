import { useState } from 'react';
import type { Check } from '../../api.generated';
import { Feedback, Field } from '../../components/ui';
import type { PreparationControls, PreparationState } from './types';

function parseChecks(text: string): Check[] {
  let value: unknown;
  try { value = JSON.parse(text); }
  catch { throw new Error('检查 JSON 格式不正确，请修正后重试。输入已保留。'); }
  if (!Array.isArray(value) || value.length === 0) throw new Error('请提供至少一项真实检查。');
  return value as Check[];
}

export function InitializationPolicy({ project, current, client, action }: PreparationControls & PreparationState) {
  const [draft, setDraft] = useState(() => ({
    version: project.version, checks: JSON.stringify(project.checks ?? [], null, 2),
    protectedPaths: project.protected_paths,
  }));
  const [error, setError] = useState('');
  function reload() {
    setDraft({ version: project.version, checks: JSON.stringify(project.checks ?? [], null, 2),
      protectedPaths: project.protected_paths });
    setError('');
  }
  function save() {
    if (draft.version !== project.version) {
      setError('项目政策版本已变化，请载入当前政策后重新核对。输入已保留。'); return;
    }
    setError('');
    void action.execute(() => client.write('/project/policy', {
      expected_version: draft.version, checks: parseChecks(draft.checks), protected_paths: draft.protectedPaths,
    }), '真实检查政策已保存。请重新分析并确认精确基线。');
  }
  return (
    <section className="panel">
      <details open={!project.checks?.length}>
        <summary>Owner 配置实际检查政策</summary>
        <p>保存会使旧基线与任务授权失效。请保持暂停，并等待分析进程停止。</p>
        <form onSubmit={(event) => { event.preventDefault(); save(); }}>
          <Field label="实际检查 JSON" hint="非空数组；包含 name、command、args、timeout_seconds（1–3600）。">
            <textarea rows={6} required value={draft.checks}
              onChange={(event) => setDraft({ ...draft, checks: event.target.value })} />
          </Field>
          <Feedback error={error} />
          <button className="secondary" disabled={action.busy || !project.paused || current?.status === 'analyzing'}>
            保存 Owner 检查政策
          </button>
          {draft.version !== project.version && (
            <button className="text-button" type="button" onClick={reload}>载入当前政策</button>
          )}
        </form>
      </details>
    </section>
  );
}
