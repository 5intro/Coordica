import { useCallback, useEffect, useRef, useState } from 'react';
import type { Snapshot } from '../api.generated';
import { ApiClient, messageOf } from '../api/client';

export function useSnapshot(client: ApiClient, interval = 3000) {
  const [snapshot, setSnapshot] = useState<Snapshot>();
  const [error, setError] = useState('');
  const [updatedAt, setUpdatedAt] = useState<Date>();
  const [loading, setLoading] = useState(true);
  const [refreshVersion, setRefreshVersion] = useState(0);
  const generation = useRef(0);
  const refresh = useCallback(() => setRefreshVersion((v) => v + 1), []);

  useEffect(() => {
    const current = ++generation.current;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    let disposed = false;
    async function poll() {
      try {
        const next = await client.snapshot(controller.signal);
        if (disposed || current !== generation.current) return;
        setSnapshot(next);
        setUpdatedAt(new Date());
        setError('');
      } catch (failure) {
        if (!disposed && current === generation.current) setError(messageOf(failure));
      } finally {
        if (!disposed && current === generation.current) {
          setLoading(false);
          timer = setTimeout(poll, interval);
        }
      }
    }
    void poll();
    return () => { disposed = true; controller.abort(); clearTimeout(timer); };
  }, [client, refreshVersion, interval]);

  return { snapshot, error, updatedAt, loading, refresh };
}
