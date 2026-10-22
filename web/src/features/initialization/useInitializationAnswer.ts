import { useState, useSyncExternalStore } from 'react';
import type { Initialization } from '../../api.generated';
import { ApiError } from '../../api/client';
import { initializationReply } from '../../api/initialization';
import type { PreparationControls } from './types';

export function useInitializationAnswer(projectID: string, current: Initialization, { client, action }: PreparationControls) {
  const session = client.initializationAnswers;
  const state = useSyncExternalStore(session.subscribe, () => session.read(projectID));
  const draft = state?.draft ?? { version: current.version, question: '', text: '' };
  const pending = state?.pending ?? null;
  const [error, setError] = useState('');
  const stale = !!draft.text && draft.version !== current.version;
  function change(text: string) {
    session.edit(projectID, { ...draft, text, version: draft.text ? draft.version : current.version });
  }
  function select(question: string) {
    session.edit(projectID, { ...draft, question, version: draft.text ? draft.version : current.version });
  }
  function reload() {
    const exists = current.result?.questions?.some((question) => question.id === draft.question);
    session.edit(projectID, { ...draft, version: current.version, question: exists ? draft.question : '' });
    setError('');
  }
  async function submit() {
    const existing = session.read(projectID)?.pending;
    if (!existing && stale) { setError('问题版本已变化，请核对当前问题后再提交。'); return; }
    if (!existing && (!draft.text.trim() || new TextEncoder().encode(draft.text).length > 16384)) {
      setError('回答不能为空，且不能超过 16 KiB UTF-8。'); return;
    }
    const request = session.begin(projectID, existing ?? {
      key: crypto.randomUUID(),
      body: {
        expected_version: draft.version, question_id: draft.question, original_text: draft.text,
        source: { session_id: 'owner-console', message_id: crypto.randomUUID() },
      },
    });
    setError('');
    const ok = await action.execute(async () => {
      try {
        const value = initializationReply(await client.write('/initialization/answer', request.body, request.key));
        if (!value) throw new ApiError('invalid_response', '初始化回执不完整，原始回答已保留，请使用同一请求重试。', 200);
        return value;
      }
      catch (failure) {
        if (failure instanceof ApiError && failure.status >= 400 && failure.status < 500
          && failure.code !== 'invalid_response') session.settle(projectID, request.key, false);
        throw failure;
      }
    }, '原始反馈已保存。Leader 将继续完善项目约定。');
    if (ok) session.settle(projectID, request.key, true);
  }
  return { draft, pending, stale, error, change, select, reload, submit };
}
