import { expect, it, vi } from 'vitest';
import type { LeaderActivityPage } from '../api.generated';
import { activityPage, followActivity } from './leaderActivity';

export const activityFixture: LeaderActivityPage = {
  version: 1, cursor: 1, oldest: 1, truncated: false, has_more: false, queried_at: '2026-10-22T10:00:00Z',
  executions: [{ ref: { kind: 'planning', id: 'plan', generation: 1, input_version: 3 }, version: 1,
    purpose: '', source_id: '', state: 'running', tree_stopped: false, settled: false }],
  events: [{ execution: { kind: 'planning', id: 'plan', generation: 1, input_version: 3 }, sequence: 1,
    key: 'one', kind: 'message', text: '检查模块边界', time: '2026-10-22T10:00:00Z' }],
};

it('rejects unbounded, malformed and out of order activity pages', () => {
  expect(activityPage(activityFixture)).toEqual(activityFixture);
  for (const bad of [{ ...activityFixture, version: 2 }, { ...activityFixture, events: null },
    { ...activityFixture, cursor: 0 }, { ...activityFixture, events: [activityFixture.events![0], activityFixture.events![0]] },
    { ...activityFixture, events: [{ ...activityFixture.events![0], text: '字'.repeat(1000) }] }]) {
    expect(() => activityPage(bad)).toThrow();
  }
});

it('parses split UTF-8 SSE frames with header-only credentials and cancels on route exit', async () => {
  const controller = new AbortController();
  const frame = new TextEncoder().encode(`id: 1\nevent: activity\ndata: ${JSON.stringify(activityFixture)}\n\n`);
  const cancel = vi.fn();
  const stream = new ReadableStream<Uint8Array>({ start(output) {
    output.enqueue(frame.slice(0, frame.length - 7)); output.enqueue(frame.slice(frame.length - 7));
  }, cancel });
  const fetch = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(stream, { headers: { 'Content-Type': 'text/event-stream' } }));
  const received = vi.fn(() => controller.abort());
  await followActivity('PRIVATE_TOKEN', 9, controller.signal, received);
  expect(received).toHaveBeenCalledWith(activityFixture);
  expect(fetch.mock.calls[0][0]).toBe('/api/v1/leader/activity/stream?after=9');
  expect(fetch.mock.calls[0][1]?.headers).toMatchObject({ Authorization: 'Bearer PRIVATE_TOKEN' });
  expect(cancel).toHaveBeenCalledOnce();
});

it('treats stream EOF as disconnected, never as finished execution', async () => {
  const stream = new ReadableStream({ start(output) { output.close(); } });
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(stream, { headers: { 'Content-Type': 'text/event-stream' } }));
  await expect(followActivity('owner', 0, new AbortController().signal, vi.fn())).rejects.toThrow('连接已断开');
});
