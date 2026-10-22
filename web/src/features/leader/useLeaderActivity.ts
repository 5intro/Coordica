import { useEffect, useRef, useState } from 'react';
import type { LeaderActivityEvent, LeaderActivityPage } from '../../api.generated';
import { activityPage } from '../../api/leaderActivity';
import { ApiClient, messageOf } from '../../api/client';

export function mergeActivity(previous: LeaderActivityEvent[], page: LeaderActivityPage): LeaderActivityEvent[] {
  const merged = new Map(previous.map((event) => [event.sequence, event]));
  (page.events ?? []).forEach((event) => merged.set(event.sequence, event));
  return [...merged.values()].sort((a, b) => a.sequence - b.sequence).slice(-200);
}

export function useLeaderActivity(client: ApiClient) {
  const [page, setPage] = useState<LeaderActivityPage>();
  const [events, setEvents] = useState<LeaderActivityEvent[]>([]);
  const [error, setError] = useState('');
  const [connected, setConnected] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const cursor = useRef(0);
  useEffect(() => {
    const controller = new AbortController();
    setConnected(false);
    setError('');
    void client.followLeader(cursor.current, controller.signal, (next) => {
      if (controller.signal.aborted) return;
      if (next.cursor < cursor.current) throw new Error('活动游标已改变，请重新载入页面。');
      cursor.current = next.cursor;
      setPage(next);
      setEvents((previous) => mergeActivity(previous, next));
      setConnected(true);
    }).catch((failure) => {
      if (!controller.signal.aborted) { setError(messageOf(failure)); setConnected(false); }
    });
    return () => controller.abort();
  }, [client, attempt]);
  return { page, events, error, connected, reconnect: () => setAttempt((value) => value + 1) };
}

// Selecting an older execution performs one bounded history read, without
// invoking a model or creating a conversation. Live events continue separately.
export function useLeaderHistory(client: ApiClient, selected: string, kind: string) {
  const [history, setHistory] = useState<LeaderActivityPage>();
  const [error, setError] = useState('');
  useEffect(() => {
    setHistory(undefined);
    setError('');
    if (!selected || !kind) return;
    const controller = new AbortController();
    const query = new URLSearchParams({ kind, id: selected, limit: '100' });
    void client.read(`/leader/activity?${query}`, controller.signal).then((value) => {
      if (controller.signal.aborted) return;
      const page = activityPage(value);
      if (page.executions?.length !== 1 || page.executions[0].ref.id !== selected || page.executions[0].ref.kind !== kind) {
        throw new Error('历史记录关联不匹配。');
      }
      setHistory(page);
    }).catch((failure) => { if (!controller.signal.aborted) setError(messageOf(failure)); });
    return () => controller.abort();
  }, [client, selected, kind]);
  return { history, error };
}
