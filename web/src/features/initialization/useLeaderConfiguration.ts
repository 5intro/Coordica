import { useCallback, useEffect, useRef, useState } from 'react';
import type { AutomationSettings, LeaderConfiguration, LeaderSelection } from '../../api.generated';
import { messageOf, type ApiClient } from '../../api/client';

export function useLeaderConfiguration(client: ApiClient, active: boolean) {
  const [configuration, setConfiguration] = useState<LeaderConfiguration>();
  const [draft, setDraft] = useState<LeaderSelection>();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const request = useRef<AbortController | undefined>(undefined);
  const reload = useCallback(async () => {
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    setLoading(true); setError('');
    try {
      const value = await client.read<LeaderConfiguration>('/leader/configuration', controller.signal);
      if (!value || !Number.isSafeInteger(value.version) || !value.selection || !Array.isArray(value.capabilities)) {
        throw new Error('Leader 配置响应不完整，请刷新重试。');
      }
      if (!controller.signal.aborted) setConfiguration(value);
    } catch (failure) {
      if (!controller.signal.aborted) setError(messageOf(failure));
    } finally {
      if (!controller.signal.aborted) setLoading(false);
    }
  }, [client]);
  useEffect(() => {
    if (active) void reload();
    return () => request.current?.abort();
  }, [active, reload]);
  const selection = draft ?? configuration?.selection;
  const capabilities = configuration?.capabilities?.filter((cap) => cap.installed && cap.available &&
    cap.structured && cap.cancel && cap.review && cap.model_discovery === 'reported') ?? [];
  const tool = capabilities.find((cap) => cap.tool === selection?.tool);
  const model = tool?.models?.find((item) => item.id === selection?.model);
  const valid = !!model && (!selection?.reasoning_effort || !!model.reasoning_efforts?.includes(selection.reasoning_effort));
  function accepted(submitted: LeaderSelection | undefined, settings?: AutomationSettings) {
    // Never let a late save receipt replace a newer local selection.
    setDraft((latest) => latest === submitted ? undefined : latest);
    if (settings) setConfiguration((current) => current && ({ ...current, version: settings.version,
      selection: { tool: settings.leader_tool, model: settings.leader_model, reasoning_effort: settings.leader_reasoning_effort } }));
    else void reload();
  }
  return { configuration, capabilities, selection, model, draft, setDraft, loading, error, valid, reload, accepted };
}
