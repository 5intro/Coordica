import type { Initialization } from '../../api.generated';

export function PreparationEvidence({ current }: { current: Initialization }) {
  return (
    <section className="panel">
      {!!current.feedback?.length && (
        <details>
          <summary>保留的原始回答 · {current.feedback.length}</summary>
          {current.feedback.map((feedback) => (
            <blockquote key={feedback.id}>
              <p className="preserve-lines">{feedback.original_text}</p>
              <details><summary>技术详情：反馈来源</summary>
                <small>v{feedback.version} · {feedback.actor_id} · {feedback.question_id || '反馈'} ·
                  {feedback.source.session_id} / {feedback.source.message_id}</small>
              </details>
            </blockquote>
          ))}
        </details>
      )}
      <details>
        <summary>源码分析与覆盖记录</summary>
        <p>已报告检查 {current.covered_paths?.length ?? 0} 个文件。</p>
        <ul>{current.result?.facts?.map((fact, index) => (
          <li key={index}><code>{fact.path}</code>：{fact.fact}</li>
        ))}</ul>
        {!!current.result?.suggestions?.length && <>
          <h3>待决定建议</h3>
          <ul>{current.result.suggestions.map((suggestion, index) => <li key={index}>{suggestion}</li>)}</ul>
        </>}
        <details><summary>已报告检查的固定源码文件</summary>
          <ul>{current.covered_paths?.map((path) => <li key={path}><code>{path}</code></li>)}</ul>
        </details>
        <details><summary>省略项与原因</summary>
          <ul>{current.omissions?.map((reason, index) => <li key={index}>{reason}</li>)}</ul>
        </details>
      </details>
      <details>
        <summary>技术详情：版本与证据</summary>
        <dl className="facts">
          <dt>草案版本</dt><dd>v{current.version}</dd>
          <dt>已确认版本</dt><dd>{current.approved_version || '尚未确认'}</dd>
          <dt>证据提交</dt><dd><code>{current.source_sha || '新项目尚无提交'}</code></dd>
          <dt>草案摘要</dt><dd><code>{current.digest || '等待分析结果'}</code></dd>
        </dl>
      </details>
    </section>
  );
}
