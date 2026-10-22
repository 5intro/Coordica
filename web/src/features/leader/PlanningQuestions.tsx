import type { Snapshot } from '../../api.generated';
import { ApiClient } from '../../api/client';
import { Feedback, Field } from '../../components/ui';
import { usePlanningAnswer } from './usePlanningAnswer';

export function PlanningQuestions({ snapshot, client, refresh }: { snapshot: Snapshot; client: ApiClient; refresh: () => void }) {
  const goal = snapshot.planning_document;
  const reply = usePlanningAnswer(snapshot.project?.id ?? '', goal, client, refresh);
  if (!reply.questions.length && !reply.pending && !reply.draft.text) return null;
  return (
    <section className="panel" aria-labelledby="leader-questions-title">
      <h2 id="leader-questions-title">规划 · 待回答</h2>
      {!goal?.leader_run_id && <p>历史问题没有执行关联，请在规划操作中重新评估后回答。</p>}
      <form onSubmit={(event) => { event.preventDefault(); void reply.submit(); }}>
        <Field label="Leader 的问题"><select disabled={!!reply.pending} value={reply.draft.question}
          onChange={(event) => reply.edit({ question: event.target.value })}>
          <option value="">选择问题</option>
          {reply.questions.map((question) => <option key={question} value={question}>{question}</option>)}
        </select></Field>
        {reply.pending && <p className="preserve-lines">正在确认的原问题：{reply.pending.body.question}</p>}
        <Field label="回复 Leader"><textarea rows={4} maxLength={16384} disabled={!!reply.pending}
          value={reply.draft.text} onChange={(event) => reply.edit({ text: event.target.value })} /></Field>
        {reply.stale && !reply.pending && <p role="status">问题已更新，原文保留。
          <button type="button" className="text-button" onClick={reply.reload}>载入当前问题</button>
        </p>}
        <Feedback {...reply.action} error={reply.error || reply.action.error} />
        <button className="secondary" disabled={reply.action.busy || !reply.pending && (reply.stale || !goal?.leader_run_id)}>
          {reply.pending ? '重试待确认回复' : '保存回答并排队评估'}
        </button>
      </form>
      <small>暂停期间仅排队，不恢复施工或增加授权。</small>
    </section>
  );
}
