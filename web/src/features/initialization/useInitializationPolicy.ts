import { useState, useSyncExternalStore } from 'react';
import type { Check } from '../../api.generated';
import { ApiError } from '../../api/client';
import { projectPolicyDraft } from '../../api/initializationPolicies';
import { validPolicyReceipt } from '../../api/policyReceipt';
import type { PreparationControls, PreparationState } from './types';

function parseChecks(text: string): Check[] {
  let value: unknown;
  try { value = JSON.parse(text); }
  catch { throw new Error('检查 JSON 格式不正确，请修正后重试。输入已保留。'); }
  if (!Array.isArray(value) || value.length === 0) throw new Error('请提供至少一项真实检查。');
  return value as Check[];
}

export function useInitializationPolicy({ project, client, action }: PreparationControls & PreparationState) {
  const session = client.initializationPolicies;
  const state = useSyncExternalStore(session.subscribe, () => session.read(project.id));
  const draft = state?.draft ?? projectPolicyDraft(project);
  const pending = state?.pending ?? null;
  const [error, setError] = useState('');
  function edit(checks: string) { session.edit(project.id, { ...draft, checks }); }
  function reload() { session.edit(project.id, projectPolicyDraft(project)); setError(''); }
  async function save() {
    const existing = session.read(project.id)?.pending;
    if (!existing && draft.version !== project.version) {
      setError('项目政策版本已变化，请载入当前政策后重新核对。输入已保留。'); return;
    }
    setError('');
    const ok = await action.execute(async () => {
      const request = existing ?? session.begin(project.id, draft, {
        key: crypto.randomUUID(), policyVersion: draft.policyVersion, body: { expected_version: draft.version,
          checks: parseChecks(draft.checks), protected_paths: draft.protectedPaths },
      });
      try {
        const value = await client.write('/project/policy', request.body, request.key);
        if (!validPolicyReceipt(value, project.id, request.body, request.policyVersion)) {
          throw new ApiError('invalid_response', '政策回执不完整，原始政策已保留，请使用同一请求重试。', 200);
        }
        session.settle(project.id, request.key, true);
      } catch (failure) {
        if (failure instanceof ApiError && failure.status >= 400 && failure.status < 500
          && failure.code !== 'invalid_response') session.settle(project.id, request.key, false);
        throw failure;
      }
    }, '真实检查政策已保存。请重新分析并确认精确基线。');
    return ok;
  }
  return { draft, pending, error, edit, reload, save };
}
