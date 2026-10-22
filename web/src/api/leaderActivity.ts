import type { LeaderActivityPage, LeaderActivityEvent, LeaderExecution } from '../api.generated';

const object = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value);
const number = (value: unknown): value is number => Number.isSafeInteger(value) && Number(value) >= 0;
function binding(value: unknown): boolean {
  return object(value) && ['planning', 'initialization'].includes(String(value.kind))
    && typeof value.id === 'string' && value.id.length <= 128 && number(value.generation) && number(value.input_version);
}
function event(value: unknown): value is LeaderActivityEvent {
  return object(value) && number(value.sequence) && binding(value.execution) && typeof value.key === 'string'
    && ['started', 'message', 'tool_started', 'tool_completed', 'plan', 'result_received', 'turn_finished', 'turn_failed', 'gap']
      .includes(String(value.kind)) && typeof value.text === 'string' && new TextEncoder().encode(value.text).length <= 2048
    && typeof value.time === 'string';
}
function execution(value: unknown): value is LeaderExecution {
  return object(value) && binding(value.ref) && number(value.version) && typeof value.purpose === 'string'
    && typeof value.source_id === 'string' && typeof value.state === 'string'
    && (value.source_kind === undefined || ['', 'conversation', 'requirement_change', 'scope_request'].includes(String(value.source_kind)))
    && typeof value.tree_stopped === 'boolean' && typeof value.settled === 'boolean';
}

export class ActivityStreamError extends Error {
  constructor(public readonly code: string, message: string) { super(message); }
}
export function activityPage(value: unknown): LeaderActivityPage {
  if (!object(value) || value.version !== 1 || !number(value.cursor) || !number(value.oldest)
    || !Array.isArray(value.events) || value.events.length > 100 || !value.events.every(event)
    || !Array.isArray(value.executions) || value.executions.length > 40 || !value.executions.every(execution)
    || typeof value.truncated !== 'boolean' || typeof value.has_more !== 'boolean' || typeof value.queried_at !== 'string'
    || value.events.some((item, index, events) => item.sequence > Number(value.cursor)
      || index > 0 && item.sequence <= events[index - 1].sequence)) throw new Error('活动契约不兼容，请更新中心与页面。');
  return value as unknown as LeaderActivityPage;
}

// Fetch streams carry the credential in a header; tokens never enter a URL.
// No progress timer or background session creation. Reconnect is explicit.
export async function followActivity(token: string, after: number, signal: AbortSignal,
  receive: (page: LeaderActivityPage) => void): Promise<void> {
  const response = await fetch(`/api/v1/leader/activity/stream?after=${after}`, {
    headers: { Accept: 'text/event-stream', Authorization: `Bearer ${token}` }, signal,
  });
  if (!response.ok) {
    const value: unknown = await response.json().catch(() => undefined);
    const code = object(value) && typeof value.code === 'string' ? value.code : 'activity_unavailable';
    throw new ActivityStreamError(code, code === 'invalid_cursor'
      ? '活动游标已失效，请重新连接。' : '活动连接不可用，请检查权限或重新连接。');
  }
  if (!response.ok || !response.headers.get('Content-Type')?.startsWith('text/event-stream') || !response.body) {
    throw new Error('活动连接不可用，请检查权限或重新连接。');
  }
  const reader = response.body.getReader();
  const decoder = new TextDecoder('utf-8', { fatal: true });
  let buffer = '';
  try {
    while (!signal.aborted) {
      const part = await reader.read();
      if (part.done) throw new Error('活动连接已断开，显示最后收到的记录。');
      buffer += decoder.decode(part.value, { stream: true });
      if (buffer.length > 512 * 1024) throw new Error('活动响应超出上限。');
      let boundary: number;
      while ((boundary = buffer.indexOf('\n\n')) >= 0) {
        const frame = buffer.slice(0, boundary);
        buffer = buffer.slice(boundary + 2);
        const data = frame.split('\n').filter((line) => line.startsWith('data: ')).map((line) => line.slice(6)).join('\n');
        if (data) receive(activityPage(JSON.parse(data)));
      }
    }
  } finally { await reader.cancel().catch(() => {}); reader.releaseLock(); }
}
