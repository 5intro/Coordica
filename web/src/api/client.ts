import type { Snapshot, Problem } from '../api.generated';

export class ApiError extends Error {
  constructor(public readonly code: string, message: string, public readonly status: number = 0) {
    super(message);
    this.name = 'ApiError';
  }
}

export class ApiClient {
  private readonly uncertainWrites = new Map<string, string>();
  constructor(private readonly token: string) {}

  private async request<T>(path: string, init: RequestInit = {}): Promise<T> {
    const controller = new AbortController();
    const abort = () => controller.abort();
    if (init.signal?.aborted) controller.abort();
    else init.signal?.addEventListener('abort', abort, { once: true });
    const timeout = setTimeout(abort, 30000);
    try {
      const response = await fetch(`/api/v1${path}`, {
        ...init,
        signal: controller.signal,
        headers: {
          Accept: 'application/json',
          ...(this.token ? { Authorization: `Bearer ${this.token}` } : {}),
          ...init.headers,
        },
      });
      let body: unknown;
      try { body = await response.json(); } catch (error) {
        if (controller.signal.aborted) throw error;
        throw new ApiError('invalid_response', '中心未返回有效的 JSON 响应。', response.status);
      }
      if (!response.ok) {
        const problem = body as Partial<Problem> | null;
        throw new ApiError(
          typeof problem?.code === 'string' ? problem.code : 'request_failed',
          typeof problem?.message === 'string' ? problem.message : `请求失败（HTTP ${response.status}）`,
          response.status,
        );
      }
      return body as T;
    } catch (error) {
      if (error instanceof ApiError || init.signal?.aborted) throw error;
      throw new ApiError('network_error', '无法连接中心。保留上次状态，请检查连接后刷新。');
    } finally {
      clearTimeout(timeout);
      init.signal?.removeEventListener('abort', abort);
    }
  }

  read<T>(path: string, signal?: AbortSignal): Promise<T> { return this.request<T>(path, { signal }); }

  async snapshot(signal?: AbortSignal): Promise<Snapshot> {
    const snapshot = await this.request<Snapshot>('/state', { signal });
    if (!snapshot || snapshot.api_version !== 'v1' || !('project' in snapshot)
      || !(['requirements', 'tasks', 'runs', 'candidates', 'nodes', 'events'] as const)
        .every((key) => snapshot[key] === null || Array.isArray(snapshot[key]))) {
      throw new ApiError('incompatible_response', '中心契约不兼容，请更新工作台。');
    }
    return snapshot;
  }

  async write<T>(path: string, body: unknown, idempotencyKey?: string, headers: Record<string, string> = {}): Promise<T> {
    const serialized = JSON.stringify(body);
    const fingerprint = `${path}\n${serialized}`;
    const key = idempotencyKey ?? this.uncertainWrites.get(fingerprint) ?? crypto.randomUUID();
    try {
      const result = await this.request<T>(path, {
        method: 'POST',
        headers: { ...headers, 'Content-Type': 'application/json', 'Idempotency-Key': key },
        body: serialized,
      });
      this.uncertainWrites.delete(fingerprint);
      return result;
    } catch (error) {
      // An absent/malformed receipt does not mean the operation failed. A manual
      // retry of the exact request reuses its key instead of creating a duplicate.
      if (!(error instanceof ApiError) || error.status === 0 || error.status >= 500 || error.code === 'invalid_response') {
        this.uncertainWrites.set(fingerprint, key);
      } else { this.uncertainWrites.delete(fingerprint); }
      throw error;
    }
  }

  export(): Promise<Snapshot> { return this.request<Snapshot>('/export'); }
}

export function messageOf(error: unknown): string {
  return error instanceof Error ? error.message : '操作失败，请查看中心状态。';
}
