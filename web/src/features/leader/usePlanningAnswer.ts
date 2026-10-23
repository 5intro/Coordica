import { useState, useSyncExternalStore } from 'react';
import type { PlanningAnswerReceipt, PlanningDocument } from '../../api.generated';
import { ApiClient, ApiError } from '../../api/client';
import { useAction } from '../../hooks/useAction';
import type { PendingPlanningAnswer } from '../../api/planningAnswers';

export function validPlanningAnswer(value: unknown, request: PendingPlanningAnswer): value is PlanningAnswerReceipt {
  const receipt = value as PlanningAnswerReceipt | null;
  const body = request.body;
  return receipt?.reply_to_run_id === body.reply_to_run_id && typeof receipt?.run?.id === 'string' && !!receipt.run.id
    && receipt.run.document_version === body.expected_version && receipt.run.state === 'queued'
    && receipt.run.generation === 1 && receipt.message?.role === 'owner' && typeof receipt.message.id === 'string' && !!receipt.message.id
    && receipt.message.document_version === body.expected_version
    && receipt.message.text === `Leader 问题：\n${body.question}\n\nOwner 原始回答：\n${body.text}`;
}

export function usePlanningAnswer(project: string, goal: PlanningDocument | null | undefined, client: ApiClient, refresh: () => void) {
  const session = client.planningAnswers;
  const state = useSyncExternalStore(session.subscribe, () => session.read(project));
  const basis = { version: goal?.version ?? 0, run: goal?.leader_run_id ?? '', question: '', text: '' };
  const draft = state?.draft ?? basis;
  const action = useAction(refresh);
  const [error, setError] = useState('');
  const questions = [...new Set([...(goal?.leader_result?.clarifications ?? []), ...(goal?.leader_result?.missing_items ?? [])])];
  const stale = draft.version !== basis.version || draft.run !== basis.run || !!draft.question && !questions.includes(draft.question);
  async function submit() {
    if (!state?.pending && (stale || !draft.question || !draft.run || !draft.text.trim())) {
      setError('请核对当前问题并填写回答。'); return;
    }
    if (new TextEncoder().encode(draft.text).length > 16384) { setError('回答不能超过 16 KiB UTF-8。'); return; }
    const request = session.begin(project, state?.pending ?? { key: crypto.randomUUID(), body: {
      expected_version: draft.version, reply_to_run_id: draft.run, question: draft.question, text: draft.text,
    } });
    setError('');
    const accepted = await action.execute(async () => {
      try {
        const value = await client.write('/planning/messages', request.body, request.key);
        if (!validPlanningAnswer(value, request)) throw new ApiError('invalid_response', '回答回执不匹配，原文已保留，请重试。', 200);
        return value;
      } catch (failure) {
        if (failure instanceof ApiError && failure.status >= 400 && failure.status < 500 && failure.code !== 'invalid_response') {
          session.settle(project, request.key, false);
        }
        throw failure;
      }
    }, '回答已保存，后续评估已排队。暂停与授权保持不变。');
    if (accepted) session.settle(project, request.key, true);
  }
  return { action, error, questions, draft, stale, pending: state?.pending, submit,
    edit: (changes: Partial<typeof draft>) => session.edit(project, { ...draft, ...changes }),
    reload: () => session.edit(project, { ...basis, text: draft.text }),
  };
}
