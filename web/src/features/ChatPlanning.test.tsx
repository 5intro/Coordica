import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { ConversationRequest } from '../api.generated';
import { ApiClient } from '../api/client';
import { centerSnapshot } from '../test/connectionInfo';
import { Planning, emptyGoal } from './Planning';
const request: ConversationRequest = { id: 'consult_a', version: 4, kind: 'proposal', original_text: 'Original user architecture suggestion', assistant_summary: 'Separate assistant summary', source: { client_id: 'client_chat', session_id: 'session_a', message_id: 'message_a' }, goal_version: 2, created_at: '2026-10-11T00:00:00Z', run_id: 'plan_a', status: 'succeeded', reply: 'Durable Leader advice', stale: false, result_sha256: 'evidence' };
describe('durable advisory consultation on Planning page', () => {
  it('renders central original, summary, source, reply, version and stale status', () => {
    render(<Planning snapshot={{ ...centerSnapshot, planning_document: { ...emptyGoal, version: 3 }, conversations: [request] }} client={new ApiClient('owner')} refresh={vi.fn()} />);
    expect(screen.getByText('Original user architecture suggestion')).toBeTruthy();
    expect(screen.getByText('助手摘要：Separate assistant summary')).toBeTruthy();
    expect(screen.getByText('Leader 建议：Durable Leader advice')).toBeTruthy();
    expect(screen.getByText(/client_chat \/ session_a \/ message_a/)).toBeTruthy();
    expect(screen.getByText(/请求 v4 · 目标 v2 · 旧目标建议/)).toBeTruthy();
    expect(screen.getByText('evidence')).toBeTruthy();
  });
  it('consults without a goal and never invents a local reply/history after an async acknowledgment', async () => {
    const client = new ApiClient('owner');
    const write = vi.spyOn(client, 'write').mockResolvedValue({ id: 'acknowledged_id', status: 'queued' });
    const refresh = vi.fn();
    render(<Planning snapshot={{ ...centerSnapshot, planning_document: null, conversations: [] }} client={client} refresh={refresh} />);
    fireEvent.change(screen.getByLabelText('纯咨询内容'), { target: { value: 'User asks for advice only' } });
    fireEvent.click(screen.getByRole('button', { name: '纯咨询 Leader（不派生任务）' }));
    await waitFor(() => expect(refresh).toHaveBeenCalled());
    expect(write).toHaveBeenCalledExactlyOnceWith('/planning/consult', expect.objectContaining({ expected_goal_version: 0, original_text: 'User asks for advice only', assistant_summary: '', source: expect.objectContaining({ client_id: 'owner', session_id: 'web' }) }), expect.any(String));
    expect(screen.queryByText('acknowledged_id')).toBeNull();
    expect(screen.getByRole('button', { name: '评估规划' })).toHaveProperty('disabled', true);
  });
  it('submits advisory proposals separately from explicit execution evaluation', async () => {
    const client = new ApiClient('owner');
    const write = vi.spyOn(client, 'write').mockResolvedValue({});
    render(<Planning snapshot={{ ...centerSnapshot, planning_document: { ...emptyGoal, version: 7 }, conversations: [] }} client={client} refresh={vi.fn()} />);
    fireEvent.change(screen.getByLabelText('纯咨询内容'), { target: { value: 'Relay a rule suggestion' } });
    fireEvent.click(screen.getByRole('button', { name: '提交架构 / 规则建议' }));
    await waitFor(() => expect(write).toHaveBeenCalledWith('/planning/propose', expect.objectContaining({ expected_goal_version: 7, original_text: 'Relay a rule suggestion' }), expect.any(String)));
    fireEvent.change(screen.getByLabelText('发送给 Leader'), { target: { value: 'Evaluate authorized execution' } });
    fireEvent.click(screen.getByRole('button', { name: '评估规划' }));
    await waitFor(() => expect(write).toHaveBeenCalledWith('/planning/evaluate', { expected_version: 7, text: 'Evaluate authorized execution' }));
    expect(screen.getByText(/可能派生任务/)).toBeTruthy();
  });
});

it('retries the entire uncertain consultation envelope and key through real ApiClient after lost ACK', async () => {
  const accepted: { body: string; key: string }[] = [];
  const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async (_url, options) => {
    accepted.push({ body: options!.body as string, key: (options!.headers as Record<string, string>)['Idempotency-Key'] });
    if (accepted.length === 1) throw new TypeError('center accepted; network ACK lost');
    return new Response(JSON.stringify({ id: 'durable_request', status: 'queued' }), { status: 200 });
  });
  const client = new ApiClient('owner');
  const view = render(<Planning snapshot={{ ...centerSnapshot, planning_document: { ...emptyGoal, version: 7 } }} client={client} refresh={vi.fn()} />);
  fireEvent.change(screen.getByLabelText('纯咨询内容'), { target: { value: 'Original explicit question' } });
  fireEvent.click(screen.getByRole('button', { name: '纯咨询 Leader（不派生任务）' }));
  await waitFor(() => expect(screen.getByText(/无法连接中心/)).toBeTruthy());
  view.rerender(<Planning snapshot={{ ...centerSnapshot, planning_document: { ...emptyGoal, version: 8 } }} client={client} refresh={vi.fn()} />);
  fireEvent.click(screen.getByRole('button', { name: '重试待确认咨询请求' }));
  await waitFor(() => expect(accepted.length).toBe(2));
  expect(accepted[1]).toEqual(accepted[0]);
  expect(JSON.parse(accepted[1].body).expected_goal_version).toBe(7);
  await waitFor(() => expect(screen.getByRole('button', { name: '纯咨询 Leader（不派生任务）' })).toHaveProperty('disabled', false));
  fireEvent.click(screen.getByRole('button', { name: '纯咨询 Leader（不派生任务）' }));
  await waitFor(() => expect(accepted.length).toBe(3));
  expect(accepted[2].key).not.toBe(accepted[1].key);
  expect(JSON.parse(accepted[2].body).source.message_id).not.toBe(JSON.parse(accepted[1].body).source.message_id);
  fetchMock.mockRestore();
});

it('enforces the server UTF-8 byte bound for multibyte consultation text', () => {
  const client = new ApiClient('owner');
  const write = vi.spyOn(client, 'write');
  render(<Planning snapshot={centerSnapshot} client={client} refresh={vi.fn()} />);
  fireEvent.change(screen.getByLabelText('纯咨询内容'), { target: { value: '中'.repeat(6000) } });
  fireEvent.click(screen.getByRole('button', { name: '纯咨询 Leader（不派生任务）' }));
  expect(write).not.toHaveBeenCalled();
  expect(screen.getByText(/不超过 16 KiB UTF-8/)).toBeTruthy();
});
