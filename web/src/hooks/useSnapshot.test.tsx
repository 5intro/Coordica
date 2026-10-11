import { act, renderHook, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Snapshot } from '../api.generated';
import { ApiClient } from '../api/client';
import { useSnapshot } from './useSnapshot';

const snapshot: Snapshot = { api_version: 'v1', project: null, tasks: null, requirements: null, runs: null, candidates: null, nodes: null, events: null };
afterEach(() => vi.restoreAllMocks());

describe('snapshot polling', () => {
  it('aborts an old request and ignores its late response after refresh', async () => {
    const client = new ApiClient('token');
    let late: (value: Snapshot) => void = () => undefined;
    const request = vi.spyOn(client, 'snapshot').mockImplementationOnce(() => new Promise((resolve) => { late = resolve; }))
      .mockResolvedValueOnce({ ...snapshot, events: [{ sequence: 2, time: '', actor: 'owner', kind: 'fresh', object_id: '', detail: '' }] });
    const { result, unmount } = renderHook(() => useSnapshot(client, 60000));
    const oldSignal = request.mock.calls[0][0];
    act(() => result.current.refresh());
    await waitFor(() => expect(result.current.snapshot?.events?.[0].sequence).toBe(2));
    expect(oldSignal?.aborted).toBe(true);
    await act(async () => late({ ...snapshot, events: [{ sequence: 1, time: '', actor: 'owner', kind: 'stale', object_id: '', detail: '' }] }));
    expect(result.current.snapshot?.events?.[0].sequence).toBe(2);
    unmount();
  });

  it('retains the last actual snapshot when the connection fails', async () => {
    const client = new ApiClient('token');
    vi.spyOn(client, 'snapshot').mockResolvedValueOnce(snapshot).mockRejectedValueOnce(new Error('中心离线'));
    const { result, unmount } = renderHook(() => useSnapshot(client, 60000));
    await waitFor(() => expect(result.current.snapshot).toEqual(snapshot));
    act(() => result.current.refresh());
    await waitFor(() => expect(result.current.error).toBe('中心离线'));
    expect(result.current.snapshot).toEqual(snapshot);
    unmount();
  });
});
