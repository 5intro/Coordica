import { useEffect, useRef, useState } from 'react';
import type { LeaderActivityEvent, LeaderActivityPage } from '../../api.generated';
import { activityPage } from '../../api/leaderActivity';
import { ApiClient, ApiError, messageOf } from '../../api/client';

export function mergeActivity(previous: LeaderActivityEvent[], page: LeaderActivityPage): LeaderActivityEvent[] {
  const merged = new Map(previous.map((event) => [event.sequence, event]));
  (page.events ?? []).forEach((event) => merged.set(event.sequence, event));
  return [...merged.values()].sort((a, b) => a.sequence - b.sequence).slice(-200);
}

export function useLeaderActivity(client: ApiClient) {
  const [page, setPage] = useState<LeaderActivityPage>();
  const [events, setEvents] = useState<LeaderActivityEvent[]>([]);
  const [truncated, setTruncated] = useState(false);
  const retained = useRef<LeaderActivityEvent[]>([]);
  const [error, setError] = useState('');
  const [connected, setConnected] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const cursor = useRef(0);
  const invalidCursor = useRef(false);
  useEffect(() => {
    const controller = new AbortController();
    setConnected(false);
    setError('');
    void client.followLeader(cursor.current, controller.signal, (next) => {
      if (controller.signal.aborted) return;
      if (next.cursor < cursor.current) throw new Error('活动游标已改变，请重新载入页面。');
      cursor.current = next.cursor;
      setPage(next);
      const count = new Set([...retained.current, ...(next.events ?? [])].map((event) => event.sequence)).size;
      retained.current = mergeActivity(retained.current, next);
      if (next.truncated || count > retained.current.length) setTruncated(true);
      setEvents(retained.current);
      setConnected(true);
    }).catch((failure) => {
      if (!controller.signal.aborted) {
        invalidCursor.current = failure instanceof ApiError && failure.code === 'invalid_cursor';
        setError(messageOf(failure)); setConnected(false);
      }
    });
    return () => controller.abort();
  }, [client, attempt]);
  function reconnect() {
    if (invalidCursor.current) {
      cursor.current = 0;
      retained.current = [];
      setEvents([]); setPage(undefined); setTruncated(false);
      invalidCursor.current = false;
    }
    setAttempt((value) => value + 1);
  }
  return { page, events, truncated, error, connected, reconnect };
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
