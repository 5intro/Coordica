import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiClient, ApiError } from './client';

afterEach(() => { vi.unstubAllGlobals(); vi.useRealTimers(); });

describe('owner API client', () => {
  it('reuses the same idempotency key when the receipt is lost', async () => {
    const fetcher = vi.fn().mockRejectedValueOnce(new TypeError('connection lost'))
      .mockImplementation(() => Promise.resolve(new Response(JSON.stringify({ id: 'task-1' }), { status: 200 })));
    vi.stubGlobal('fetch', fetcher);
    const client = new ApiClient('private-owner-token');
    const input = { title: '可靠提交' };
    await expect(client.write('/tasks', input)).rejects.toBeInstanceOf(ApiError);
    await expect(client.write('/tasks', input)).resolves.toEqual({ id: 'task-1' });
    const first = fetcher.mock.calls[0][1] as RequestInit;
    const retry = fetcher.mock.calls[1][1] as RequestInit;
    expect(first.headers).toMatchObject({ Authorization: 'Bearer private-owner-token' });
    expect((first.headers as Record<string, string>)['Idempotency-Key']).toBe((retry.headers as Record<string, string>)['Idempotency-Key']);
    await client.write('/tasks', input);
    expect((fetcher.mock.calls[2][1].headers as Record<string, string>)['Idempotency-Key']).not.toBe((first.headers as Record<string, string>)['Idempotency-Key']);
  });

  it('surfaces a stale-version rejection without treating it as success', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ code: 'stale_version', message: '版本已变化' }), { status: 409 })));
    await expect(new ApiClient('token').write('/tasks/task-1/approve', { expected_version: 2 }))
      .rejects.toMatchObject({ code: 'stale_version', message: '版本已变化', status: 409 });
  });

  it('rejects incompatible state rather than manufacturing empty records', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ api_version: 'v1', project: null }), { status: 200 })));
    await expect(new ApiClient('token').snapshot()).rejects.toMatchObject({ code: 'incompatible_response' });
  });

  it('also times out a response body that stalls after the headers', async () => {
    vi.useFakeTimers();
    vi.stubGlobal('fetch', vi.fn().mockImplementation((_url: string, init: RequestInit) => Promise.resolve({
      ok: true,
      status: 200,
      json: () => new Promise((_resolve, reject) => init.signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')))),
    })));
    const result = expect(new ApiClient('token').export()).rejects.toMatchObject({ code: 'network_error' });
    await vi.advanceTimersByTimeAsync(30000);
    await result;
  });
});
