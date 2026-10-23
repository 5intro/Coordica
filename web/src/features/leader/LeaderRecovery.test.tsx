import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import type { LeaderActivityPage } from '../../api.generated';
import { ApiClient, ApiError } from '../../api/client';
import { preparationSnapshot } from '../../test/preparation';
import { LeaderActivityPanel } from './LeaderActivityPanel';
import { Planning } from './LeaderWorkspace';

function page(): LeaderActivityPage {
  const init = { kind: 'initialization', id: 'old', generation: 1, input_version: 1 };
  const plan = { kind: 'planning', id: 'active', generation: 1, input_version: 2 };
  return { version: 1, cursor: 10, oldest: 1, has_more: false, truncated: false, queried_at: '',
    executions: [{ ref: init, version: 1, state: 'succeeded', purpose: 'initialization', source_id: '', tree_stopped: true, settled: true },
      { ref: plan, version: 1, state: 'running', purpose: '', source_id: '', tree_stopped: false, settled: false }],
    events: [{ sequence: 1, execution: init, key: 'i', kind: 'message', text: '历史初始化', time: '' },
      { sequence: 10, execution: plan, key: 'p', kind: 'message', text: '当前规划工作', time: '' }] };
}

it('defaults to active planning instead of the retained initialization', () => {
  const client = new ApiClient('owner');
  let receive!: (v: LeaderActivityPage) => void;
  vi.spyOn(client, 'followLeader').mockImplementation(async (_after, _signal, next) => { receive = next; });
  render(<LeaderActivityPanel client={client} />);
  act(() => receive(page()));
  expect(screen.getByLabelText('查看记录')).toHaveProperty('value', 'active');
  expect(screen.getByText('当前规划工作')).toBeTruthy();
  expect(screen.queryByText('历史初始化')).toBeNull();
});

it('resets a future cursor and its retained timeline only after explicit invalid-cursor reconnect', async () => {
  const client = new ApiClient('owner');
  let receive!: (v: LeaderActivityPage) => void;
  let reject!: (e: Error) => void;
  const follow = vi.spyOn(client, 'followLeader').mockImplementation((_after, _signal, next) => {
    receive = next;
    return new Promise((_resolve, fail) => { reject = fail; });
  });
  render(<LeaderActivityPanel client={client} />);
  act(() => receive(page()));
  await act(async () => reject(new ApiError('invalid_cursor', '旧游标超过数据库范围', 400)));
  fireEvent.click(screen.getByRole('button', { name: '重新连接' }));
  await waitFor(() => expect(follow.mock.calls[1][0]).toBe(0));
  expect(screen.queryByText('当前规划工作')).toBeNull();
  act(() => receive({ ...page(), cursor: 2, events: [] }));
  expect(screen.getByText('已连接')).toBeTruthy();
});

it('opens planning controls for an existing run deep link', () => {
  const client = new ApiClient('owner');
  vi.spyOn(client, 'followLeader').mockImplementation(async () => {});
  render(<Planning client={client} snapshot={preparationSnapshot} refresh={vi.fn()} initialId="linked-run" />);
  expect(screen.getByText('目标、咨询与规划操作').parentElement).toHaveProperty('open', true);
});

it('labels scope assessments distinctly from ordinary goal planning', () => {
  const client = new ApiClient('owner');
  let receive!: (v: LeaderActivityPage) => void;
  vi.spyOn(client, 'followLeader').mockImplementation(async (_after, _signal, next) => { receive = next; });
  render(<LeaderActivityPanel client={client} />);
  const value = page();
  value.executions = [{ ...value.executions![1], source_id: 'scope-request', source_kind: 'scope_request' }];
  act(() => receive(value));
  expect(screen.getByRole('option', { name: /范围申请评估/ })).toBeTruthy();
  expect(screen.queryByRole('option', { name: /目标规划/ })).toBeNull();
});

it('recovers from a restored-database cursor error through the real HTTP client', async () => {
  const stream = (value: LeaderActivityPage) => new Response(`data: ${JSON.stringify(value)}\n\n`, {
    headers: { 'Content-Type': 'text/event-stream' },
  });
  const fetcher = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(stream(page()))
    .mockResolvedValueOnce(new Response(JSON.stringify({ code: 'invalid_cursor' }), { status: 400 }))
    .mockResolvedValueOnce(stream({ ...page(), cursor: 0, oldest: 0, events: [] }));
  render(<LeaderActivityPanel client={new ApiClient('owner')} />);
  await screen.findByText(/活动连接已断开/);
  fireEvent.click(screen.getByRole('button', { name: '重新连接' }));
  await screen.findByText(/活动游标已失效/);
  fireEvent.click(screen.getByRole('button', { name: '重新连接' }));
  await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(3));
  expect(fetcher.mock.calls.map((call) => call[0])).toEqual([
    '/api/v1/leader/activity/stream?after=0', '/api/v1/leader/activity/stream?after=10', '/api/v1/leader/activity/stream?after=0',
  ]);
  expect(screen.queryByText('当前规划工作')).toBeNull();
});
