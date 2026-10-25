import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { ApiClient, ApiError } from '../../api/client';
import type { LeaderActivityPage, PlanningMessageRequest } from '../../api.generated';
import { preparationSnapshot } from '../../test/preparation';
import { emptyGoal } from '../Planning';
import { PlanningQuestions } from './PlanningQuestions';
import { LeaderActivityPanel } from './LeaderActivityPanel';
import { LeaderQuestions } from './LeaderQuestions';

const snapshot = { ...preparationSnapshot, planning_document: { ...emptyGoal, version: 3, leader_run_id: 'accepted',
  leader_result: { reply: '还需确认', ready: false, missing_items: [], clarifications: ['哪种数据库？'], tasks: [], scopes: [], major_decisions: [] } } };
function receipt(body: unknown) {
  const request = body as PlanningMessageRequest;
  return { reply_to_run_id: request.reply_to_run_id, run: { id: 'queued', generation: 1, document_version: request.expected_version, state: 'queued' },
    message: { id: 'message', role: 'owner', document_version: request.expected_version,
      text: `Leader 问题：\n${request.question}\n\nOwner 原始回答：\n${request.text}` } };
}

it('binds a reply to exact result and retains unknown envelopes across route remounts', async () => {
  const client = new ApiClient('owner');
  const write = vi.spyOn(client, 'write').mockRejectedValueOnce(new ApiError('network_error', '回执丢失'))
    .mockImplementation(async (_path, body) => receipt(body));
  const props = { client, snapshot, refresh: vi.fn() };
  const view = render(<PlanningQuestions {...props} />);
  fireEvent.change(screen.getByLabelText('Leader 的问题'), { target: { value: '哪种数据库？' } });
  fireEvent.change(screen.getByLabelText('回复 Leader'), { target: { value: 'SQLite 原文' } });
  fireEvent.click(screen.getByRole('button', { name: '保存回答并排队评估' }));
  await screen.findByText('回执丢失');
  const original = write.mock.calls[0];
  view.unmount();
  render(<PlanningQuestions {...props} snapshot={{ ...snapshot,
    planning_document: { ...snapshot.planning_document, leader_run_id: 'new-result-same-goal' } }} />);
  fireEvent.click(screen.getByRole('button', { name: '重试待确认回复' }));
  await waitFor(() => expect(write).toHaveBeenCalledTimes(2));
  expect(write.mock.calls[1]).toEqual(original);
  await waitFor(() => expect(client.planningAnswers.read(snapshot.project!.id)).toBeUndefined());
});

it('requires explicit reloading of a changed Leader result without deleting prose', () => {
  const client = new ApiClient('owner');
  const write = vi.spyOn(client, 'write');
  const props = { client, snapshot, refresh: vi.fn() };
  const view = render(<PlanningQuestions {...props} />);
  fireEvent.change(screen.getByLabelText('Leader 的问题'), { target: { value: '哪种数据库？' } });
  fireEvent.change(screen.getByLabelText('回复 Leader'), { target: { value: 'SQLite 原文' } });
  view.rerender(<PlanningQuestions {...props} snapshot={{ ...snapshot,
    planning_document: { ...snapshot.planning_document, leader_run_id: 'replacement' } }} />);
  expect(screen.getByRole<HTMLButtonElement>('button', { name: '保存回答并排队评估' }).disabled).toBe(true);
  fireEvent.click(screen.getByRole('button', { name: '载入当前问题' }));
  expect((screen.getByLabelText('回复 Leader') as HTMLTextAreaElement).value).toBe('SQLite 原文');
  expect(write).not.toHaveBeenCalled();
});

it('keeps a mismatched receipt pending for exact retry', async () => {
  const client = new ApiClient('owner');
  vi.spyOn(client, 'write').mockResolvedValue(receipt({ expected_version: 999, text: 'other', question: 'other' }));
  render(<PlanningQuestions client={client} snapshot={snapshot} refresh={vi.fn()} />);
  fireEvent.change(screen.getByLabelText('Leader 的问题'), { target: { value: '哪种数据库？' } });
  fireEvent.change(screen.getByLabelText('回复 Leader'), { target: { value: 'SQLite' } });
  fireEvent.click(screen.getByRole('button', { name: '保存回答并排队评估' }));
  await screen.findByRole('alert');
  expect(client.planningAnswers.read(snapshot.project!.id)?.pending?.body.text).toBe('SQLite');
});

it('retains public messages on disconnect and resumes at the last cursor', async () => {
  const client = new ApiClient('owner');
  let receive!: (page: LeaderActivityPage) => void;
  let disconnect!: (error: Error) => void;
  const follow = vi.spyOn(client, 'followLeader').mockImplementation((_after, _signal, callback) => {
    receive = callback;
    return new Promise((_resolve, reject) => { disconnect = reject; });
  });
  const view = render(<LeaderActivityPanel client={client} />);
  const page: LeaderActivityPage = { version: 1, cursor: 4, oldest: 1, has_more: false, truncated: false, queried_at: '',
    executions: [{ ref: { kind: 'planning', id: 'run', generation: 1, input_version: 3 }, version: 1,
      purpose: '', source_id: '', state: 'running', tree_stopped: false, settled: false }],
    events: [{ execution: { kind: 'planning', id: 'run', generation: 1, input_version: 3 }, sequence: 4,
      key: 'four', kind: 'message', text: '正在分析边界', time: '' }] };
  act(() => receive(page));
  await act(async () => disconnect(new Error('断开连接')));
  expect(screen.getByText('正在分析边界')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: '重新连接' }));
  await waitFor(() => expect(follow.mock.calls[1][0]).toBe(4));
  const signal = follow.mock.calls[1][1];
  view.unmount();
  expect(signal.aborted).toBe(true);
});

it('shows initialization answers and directs runtime decisions to their fenced handling flow', () => {
  const client = new ApiClient('owner');
  render(<LeaderQuestions client={client} refresh={vi.fn()} snapshot={{ ...preparationSnapshot,
    decisions: [{ id: 'decision', version: 2, source_kind: 'task', source_id: 'task', source_version: 7,
      document_version: 1, authorization_version: 1, kind: 'scope_wait_stop', evidence: '需要确认原运行已停止',
      actions: ['confirm_run_stopped'], status: 'pending', resolution: '' }] }} />);
  expect(screen.getByLabelText('原始回答 / 反馈')).toBeTruthy();
  expect(screen.getByRole('link', { name: '查看并处理' }).getAttribute('href')).toBe('#blocks?id=decision%3Adecision');
  expect(screen.queryByRole('button', { name: /确认停止/ })).toBeNull();
});

it('loads selected older activity once without opening another execution or conversation', async () => {
  const client = new ApiClient('owner');
  const ref = { kind: 'planning', id: 'old', generation: 1, input_version: 3 };
  const old = { ref, version: 1, purpose: '', source_id: '', state: 'succeeded', tree_stopped: true, settled: true };
  const page: LeaderActivityPage = { version: 1, cursor: 3, oldest: 1, has_more: false, truncated: false, queried_at: '',
    executions: [{ ...old, ref: { ...ref, id: 'new' } }, old], events: [] };
  const follow = vi.spyOn(client, 'followLeader').mockImplementation(async (_after, _signal, receive) => { receive(page); });
  const read = vi.spyOn(client, 'read').mockResolvedValue({ ...page, executions: [old], events: [{
    execution: ref, sequence: 1, key: 'one', kind: 'message', text: '历史公开活动', time: '',
  }] });
  const write = vi.spyOn(client, 'write');
  render(<LeaderActivityPanel client={client} />);
  fireEvent.change(screen.getByLabelText('查看记录'), { target: { value: 'old' } });
  await screen.findByText('历史公开活动');
  expect(read.mock.calls[0][0]).toBe('/leader/activity?kind=planning&id=old&limit=100');
  expect(follow).toHaveBeenCalledOnce();
  expect(write).not.toHaveBeenCalled();
});
