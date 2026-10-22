import { useRef, useState } from 'react';
import type { Initialization, InitializationAnswer } from '../../api.generated';
import { ApiError } from '../../api/client';
import type { PreparationControls } from './types';

type Draft = { version: number; question: string; text: string };
type Pending = { key: string; body: InitializationAnswer };

export function useInitializationAnswer(current: Initialization, { client, action }: PreparationControls) {
  const [draft, setDraft] = useState<Draft>({ version: current.version, question: '', text: '' });
  const [pending, setPending] = useState<Pending | null>(null);
  const pendingRef = useRef<Pending | null>(null);
  const [error, setError] = useState('');
  const stale = !!draft.text && draft.version !== current.version;
  function remember(value: Pending | null) { pendingRef.current = value; setPending(value); }
  function change(text: string) {
    if (pendingRef.current) return;
    setDraft({ ...draft, text, version: draft.text ? draft.version : current.version });
  }
  function select(question: string) {
    if (!pendingRef.current) setDraft({ ...draft, question, version: draft.text ? draft.version : current.version });
  }
  function reload() {
    const exists = current.result?.questions?.some((question) => question.id === draft.question);
    setDraft({ ...draft, version: current.version, question: exists ? draft.question : '' });
    setError('');
  }
  async function submit() {
    if (!pendingRef.current && stale) { setError('问题版本已变化，请核对当前问题后再提交。'); return; }
    if (!pendingRef.current && (!draft.text.trim() || new TextEncoder().encode(draft.text).length > 16384)) {
      setError('回答不能为空，且不能超过 16 KiB UTF-8。'); return;
    }
    const request = pendingRef.current ?? {
      key: crypto.randomUUID(),
      body: {
        expected_version: draft.version, question_id: draft.question, original_text: draft.text,
        source: { session_id: 'owner-console', message_id: crypto.randomUUID() },
      },
    };
    remember(request);
    setError('');
    const ok = await action.execute(async () => {
      try { return await client.write('/initialization/answer', request.body, request.key); }
      catch (failure) {
        if (failure instanceof ApiError && failure.status >= 400 && failure.status < 500
          && failure.code !== 'invalid_response') remember(null);
        throw failure;
      }
    }, '原始反馈已保存。Leader 将继续完善项目约定。');
    if (ok) {
      remember(null);
      setDraft({ version: current.version, question: '', text: '' });
    }
  }
  return { draft, pending, stale, error, change, select, reload, submit };
}
