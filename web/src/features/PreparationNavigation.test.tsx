import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { ApiClient, ApiError } from '../api/client';
import { readyInitialization } from '../test/initialization';
import { preparationSnapshot as snapshot } from '../test/preparation';
import { ProjectInitialization } from './ProjectInitialization';

const failures = [
  new ApiError('network_error', '回执丢失'),
  new ApiError('request_failed', '暂时失败', 503),
  new ApiError('invalid_response', '回执损坏', 200),
];

it.each(failures)('recovers the exact uncertain answer after route unmount: $code', async (failure) => {
  const client = new ApiClient('owner');
  const write = vi.spyOn(client, 'write').mockRejectedValueOnce(failure).mockResolvedValue(readyInitialization);
  const props = { client, refresh: vi.fn() };
  const view = render(<ProjectInitialization {...props} snapshot={snapshot} />);
  fireEvent.change(screen.getByLabelText('原始回答 / 反馈'), { target: { value: '原始私有回答' } });
  fireEvent.click(screen.getByRole('button', { name: '保存原文并继续访谈' }));
  await screen.findByText(failure.message);
  const request = write.mock.calls[0];
  view.unmount();
  render(<ProjectInitialization {...props} snapshot={{ ...snapshot, initialization: {
    ...readyInitialization, version: 3, status: 'analyzing',
  } }} />);
  expect(screen.getByLabelText('原始回答 / 反馈')).toHaveProperty('value', '原始私有回答');
  expect(screen.getByLabelText('原始回答 / 反馈')).toHaveProperty('disabled', true);
  fireEvent.click(screen.getByRole('button', { name: '重试待确认回答' }));
  await waitFor(() => expect(write).toHaveBeenCalledTimes(2));
  expect(write.mock.calls[1]).toEqual(request);
  await waitFor(() => expect(client.initializationAnswers.read(snapshot.project!.id)).toBeUndefined());
});

it('does not leave a duplicate draft when an in-flight answer succeeds after navigation', async () => {
  const client = new ApiClient('owner');
  let accept!: () => void;
  const receipt = new Promise((resolve) => { accept = () => resolve(readyInitialization); });
  const write = vi.spyOn(client, 'write').mockReturnValue(receipt);
  const props = { client, refresh: vi.fn(), snapshot };
  const view = render(<ProjectInitialization {...props} />);
  fireEvent.change(screen.getByLabelText('原始回答 / 反馈'), { target: { value: '已经发出的回答' } });
  fireEvent.click(screen.getByRole('button', { name: '保存原文并继续访谈' }));
  await waitFor(() => expect(write).toHaveBeenCalledTimes(1));
  view.unmount();
  render(<ProjectInitialization {...props} />);
  expect(screen.getByLabelText('原始回答 / 反馈')).toHaveProperty('disabled', true);
  await act(async () => { accept(); await receipt; });
  await waitFor(() => expect(screen.getByLabelText('原始回答 / 反馈')).toHaveProperty('value', ''));
  expect(screen.queryByRole('button', { name: '重试待确认回答' })).toBeNull();
  expect(write).toHaveBeenCalledTimes(1);
});

it('retains an unsent answer and its old version after leaving the preparation page', () => {
  const client = new ApiClient('owner');
  const write = vi.spyOn(client, 'write');
  const props = { client, refresh: vi.fn() };
  const view = render(<ProjectInitialization {...props} snapshot={snapshot} />);
  fireEvent.change(screen.getByLabelText('原始回答 / 反馈'), { target: { value: '旧问题的回答' } });
  view.unmount();
  render(<ProjectInitialization {...props} snapshot={{ ...snapshot, initialization: { ...readyInitialization, version: 3 } }} />);
  fireEvent.click(screen.getByRole('button', { name: '保存原文并继续访谈' }));
  expect(write).not.toHaveBeenCalled();
  expect(screen.getByRole('alert').textContent).toContain('问题版本已变化');
  expect(screen.getByLabelText('原始回答 / 反馈')).toHaveProperty('value', '旧问题的回答');
});

it('keeps an incomplete JSON receipt uncertain and can retry it after navigation', async () => {
  const client = new ApiClient('owner');
  const write = vi.spyOn(client, 'write').mockResolvedValueOnce({}).mockResolvedValue(readyInitialization);
  const props = { client, refresh: vi.fn(), snapshot };
  const view = render(<ProjectInitialization {...props} />);
  fireEvent.change(screen.getByLabelText('原始回答 / 反馈'), { target: { value: '不能丢弃的原文' } });
  fireEvent.click(screen.getByRole('button', { name: '保存原文并继续访谈' }));
  await screen.findByText('初始化回执不完整，原始回答已保留，请使用同一请求重试。');
  const original = write.mock.calls[0];
  view.unmount();
  render(<ProjectInitialization {...props} />);
  fireEvent.click(screen.getByRole('button', { name: '重试待确认回答' }));
  await waitFor(() => expect(write).toHaveBeenCalledTimes(2));
  expect(write.mock.calls[1]).toEqual(original);
});
