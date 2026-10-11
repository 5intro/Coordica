import { useRef, useState } from 'react';
import { messageOf } from '../api/client';

export function useAction(refresh: () => void) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const inFlight = useRef(false);
  async function execute(operation: () => Promise<unknown>, success: string): Promise<boolean> {
    if (inFlight.current) return false;
    inFlight.current = true;
    setBusy(true); setError(''); setNotice('');
    try {
      await operation();
      setNotice(success);
      refresh();
      return true;
    } catch (failure) {
      setError(messageOf(failure));
      refresh();
      return false;
    } finally { inFlight.current = false; setBusy(false); }
  }
  return { busy, error, notice, execute };
}
