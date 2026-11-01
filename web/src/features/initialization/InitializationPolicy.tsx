import { Feedback, Field } from '../../components/ui';
import { useInitializationPolicy } from './useInitializationPolicy';
import type { PreparationControls, PreparationState } from './types';

export function InitializationPolicy({ project, current, client, action }: PreparationControls & PreparationState) {
  const { draft, pending, error, edit, reload, save } = useInitializationPolicy({ project, current, client, action });
  return (
    <section className="panel">
      <details open={!project.checks?.length}>
        <summary>Owner 配置实际检查政策</summary>
        <p>保存会使旧基线与任务授权失效。请保持暂停，并等待分析进程停止。</p>
        <form onSubmit={(event) => { event.preventDefault(); void save(); }}>
          <Field label="实际检查 JSON" hint="包含 name、command、args、timeout_seconds（1–3600）。required_paths 声明必需输入；output_paths 声明构建、依赖和缓存输出，例如 dist/**、node_modules/**。输出声明不允许修改已跟踪源码。">
            <textarea rows={6} required disabled={!!pending} value={draft.checks}
              onChange={(event) => edit(event.target.value)} />
          </Field>
          {pending && <p role="status">政策结果待确认。重试会保留原始政策、版本与同一请求身份。</p>}
          <Feedback error={error} />
          <button className="secondary" disabled={action.busy || !pending && (!project.paused || current?.status === 'analyzing')}>
            {pending ? '重试待确认政策' : '保存 Owner 检查政策'}
          </button>
          {!pending && draft.version !== project.version && (
            <button className="text-button" type="button" onClick={reload}>载入当前政策</button>
          )}
        </form>
      </details>
    </section>
  );
}
