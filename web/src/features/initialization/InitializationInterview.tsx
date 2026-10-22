import type { Initialization } from '../../api.generated';
import { Feedback, Field } from '../../components/ui';
import { useInitializationAnswer } from './useInitializationAnswer';
import type { PreparationControls } from './types';

export function InitializationInterview(props: PreparationControls & { current: Initialization }) {
  const { current, action } = props;
  const answer = useInitializationAnswer(current, props);
  const canAnswer = current.status === 'interview' || current.status === 'awaiting_owner';
  if (!canAnswer && !answer.pending) return null;
  const questions = current.result?.questions ?? [];
  return (
    <section className="panel" aria-labelledby="preparation-interview">
      <h2 id="preparation-interview">问题与原始反馈</h2>
      {questions.map((question) => (
        <div className="detail-section" key={question.id}>
          <h3>{question.text}</h3>
          {!!question.prerequisites?.length && <small>已确定前提：{question.prerequisites.join('、')}</small>}
          <button className="secondary" disabled={!!answer.pending} onClick={() => answer.select(question.id)}>回答此问题</button>
        </div>
      ))}
      <form onSubmit={(event) => { event.preventDefault(); void answer.submit(); }}>
        <Field label="当前问题">
          <select value={answer.draft.question} disabled={!!answer.pending}
            onChange={(event) => answer.select(event.target.value)}>
            <option value="">补充原始反馈</option>
            {questions.map((question) => <option key={question.id} value={question.id}>{question.text}</option>)}
          </select>
        </Field>
        <Field label="原始回答 / 反馈">
          <textarea required maxLength={16384} disabled={!!answer.pending} rows={5}
            value={answer.draft.text} onChange={(event) => answer.change(event.target.value)} />
        </Field>
        {answer.pending && <p role="status">尚未收到确认。重试保留原始回答、问题版本和同一请求身份。</p>}
        {answer.stale && !answer.pending && (
          <p role="status">问题版本已更新。
            <button className="text-button" type="button" onClick={answer.reload}>核对当前问题版本</button>
          </p>
        )}
        <Feedback error={answer.error} />
        <button className="secondary" disabled={action.busy || !answer.draft.text.trim()}>
          {answer.pending ? '重试待确认回答' : '保存原文并继续访谈'}
        </button>
      </form>
    </section>
  );
}
