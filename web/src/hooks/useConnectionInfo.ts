import { useCallback, useEffect, useState } from 'react';
import type { ConnectionInfo } from '../api.generated';
import { ApiClient, messageOf } from '../api/client';
import { validateConnectionInfo } from '../api/connection';

export function useConnectionInfo(client: ApiClient) {
  const [info, setInfo] = useState<ConnectionInfo>();
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [revision, setRevision] = useState(0);
  const reload = useCallback(() => setRevision((value) => value + 1), []);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError('');
    void client.read<ConnectionInfo>('/system/connection', controller.signal)
      .then((value) => { if (!controller.signal.aborted) setInfo(validateConnectionInfo(value)); })
      .catch((failure) => { if (!controller.signal.aborted) setError(messageOf(failure)); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [client, revision]);
  return { info, error, loading, reload };
}
