import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { ApiClient, ApiError } from '../api/client';
import { readyInitialization } from '../test/initialization';
import { preparationSnapshot as snapshot } from '../test/preparation';
import { ProjectInitialization } from './ProjectInitialization';

function controls() {
  const client = new ApiClient('owner');
  return { client, write: vi.spyOn(client, 'write').mockResolvedValue(readyInitialization), refresh: vi.fn() };
}

it('shows the confirmed agreement rather than labelling it an unapproved draft', () => {
  const { client, write, refresh } = controls();
  const baseline = { ...readyInitialization.result!.baseline, requirements: '已确认的精确需求' };
  render(<ProjectInitialization client={client} refresh={refresh} snapshot={{ ...snapshot, initialization: {
    ...readyInitialization, status: 'initialized', approved_version: 2,
    approved_digest: readyInitialization.digest, approved_baseline: baseline,
  } }} />);
  expect(screen.getByRole('heading', { name: '已确认项目约定' })).toBeTruthy();
  expect(screen.getByText('已确认的精确需求')).toBeTruthy();
  expect(screen.queryByRole('heading', { name: '待确认草案' })).toBeNull();
  expect(screen.queryByRole('button', { name: '确认已审阅的共享基线' })).toBeNull();
  expect(write).not.toHaveBeenCalled();
});

it('keeps an unsent answer attached to its original question version across MCP updates', async () => {
  const { client, write, refresh } = controls();
  const view = render(<ProjectInitialization snapshot={snapshot} client={client} refresh={refresh} />);
  fireEvent.change(screen.getByLabelText('原始回答 / 反馈'), { target: { value: '保留我的回答原文' } });
  const current = { ...readyInitialization, version: 3,
    result: { ...readyInitialization.result!, initialization_version: 3 } };
  view.rerender(<ProjectInitialization snapshot={{ ...snapshot, initialization: current }} client={client} refresh={refresh} />);
  fireEvent.change(screen.getByLabelText('当前问题'), { target: { value: '' } });
  fireEvent.click(screen.getByRole('button', { name: '保存原文并继续访谈' }));
  expect(write).not.toHaveBeenCalled();
  expect(screen.getByRole('alert').textContent).toContain('问题版本已变化');
  expect(screen.getByLabelText('原始回答 / 反馈')).toHaveProperty('value', '保留我的回答原文');
  fireEvent.click(screen.getByRole('button', { name: '核对当前问题版本' }));
  fireEvent.click(screen.getByRole('button', { name: '保存原文并继续访谈' }));
  await waitFor(() => expect(write).toHaveBeenCalledExactlyOnceWith('/initialization/answer',
    expect.objectContaining({ expected_version: 3, original_text: '保留我的回答原文' }), expect.any(String)));
});

it('retries an uncertain answer with the same body and identity after the center advances', async () => {
  const { client, write, refresh } = controls();
  write.mockRejectedValueOnce(new ApiError('network_error', '回执丢失')).mockResolvedValue(readyInitialization);
  const view = render(<ProjectInitialization snapshot={snapshot} client={client} refresh={refresh} />);
  fireEvent.change(screen.getByLabelText('原始回答 / 反馈'), { target: { value: '原始回答' } });
  fireEvent.click(screen.getByRole('button', { name: '保存原文并继续访谈' }));
  await screen.findByText('回执丢失');
  const original = write.mock.calls[0];
  view.rerender(<ProjectInitialization snapshot={{ ...snapshot, initialization: {
    ...readyInitialization, version: 3, status: 'analyzing',
  } }} client={client} refresh={refresh} />);
  expect(screen.getByLabelText('原始回答 / 反馈')).toHaveProperty('disabled', true);
  fireEvent.click(screen.getByRole('button', { name: '重试待确认回答' }));
  await waitFor(() => expect(write).toHaveBeenCalledTimes(2));
  expect(write.mock.calls[1]).toEqual(original);
  expect(write.mock.calls[1][1]).toMatchObject({ expected_version: 2, original_text: '原始回答' });
  expect(write.mock.calls.map(([path]) => path)).toEqual(['/initialization/answer', '/initialization/answer']);
});

it('retains a rejected answer for editing and counts the UTF-8 limit', async () => {
  const { client, write, refresh } = controls();
  write.mockRejectedValue(new ApiError('version_conflict', '请核对问题', 409));
  render(<ProjectInitialization snapshot={snapshot} client={client} refresh={refresh} />);
  const input = screen.getByLabelText('原始回答 / 反馈');
  fireEvent.change(input, { target: { value: '中'.repeat(6000) } });
  fireEvent.click(screen.getByRole('button', { name: '保存原文并继续访谈' }));
  expect(write).not.toHaveBeenCalled();
  expect(screen.getByRole('alert').textContent).toContain('16 KiB');
  fireEvent.change(input, { target: { value: '保留输入' } });
  fireEvent.click(screen.getByRole('button', { name: '保存原文并继续访谈' }));
  await screen.findByText('请核对问题');
  expect(input).toHaveProperty('disabled', false);
  expect(input).toHaveProperty('value', '保留输入');
});

it('reports malformed policy JSON without losing input or calling the server', async () => {
  const { client, write, refresh } = controls();
  render(<ProjectInitialization snapshot={{ ...snapshot, project: {
    ...snapshot.project!, checks: [],
  } }} client={client} refresh={refresh} />);
  const input = screen.getByLabelText(/实际检查 JSON/);
  fireEvent.change(input, { target: { value: '{broken' } });
  fireEvent.click(screen.getByRole('button', { name: '保存 Owner 检查政策' }));
  expect(await screen.findByRole('alert')).toHaveProperty('textContent', '检查 JSON 格式不正确，请修正后重试。输入已保留。');
  expect(write).not.toHaveBeenCalled();
  expect(input).toHaveProperty('value', '{broken');
});

it('does not save an old policy draft or protection paths against a new project version', async () => {
  const { client, write, refresh } = controls();
  const state = { ...snapshot, project: { ...snapshot.project!, checks: [] } };
  const view = render(<ProjectInitialization snapshot={state} client={client} refresh={refresh} />);
  const input = screen.getByLabelText(/实际检查 JSON/);
  const checks = JSON.stringify([{ name: 'test', command: 'go', args: ['test'], timeout_seconds: 30 }]);
  fireEvent.change(input, { target: { value: checks } });
  view.rerender(<ProjectInitialization snapshot={{ ...state, project: {
    ...state.project, version: 2, protected_paths: ['new/**'],
  } }} client={client} refresh={refresh} />);
  fireEvent.click(screen.getByRole('button', { name: '保存 Owner 检查政策' }));
  expect(write).not.toHaveBeenCalled();
  expect(screen.getByRole('alert').textContent).toContain('政策版本已变化');
  fireEvent.click(screen.getByRole('button', { name: '载入当前政策' }));
  fireEvent.change(input, { target: { value: checks } });
  fireEvent.click(screen.getByRole('button', { name: '保存 Owner 检查政策' }));
  await waitFor(() => expect(write).toHaveBeenCalledWith('/project/policy', {
    expected_version: 2, checks: JSON.parse(checks), protected_paths: ['new/**'],
  }, expect.any(String)));
});

it('does not reuse stop evidence after the recorded operation version changes', () => {
  const { client, write, refresh } = controls();
  const current = { ...readyInitialization, status: 'analyzing', operation_state: 'unknown_live' };
  const view = render(<ProjectInitialization snapshot={{ ...snapshot, initialization: current }}
    client={client} refresh={refresh} />);
  expect(screen.getByRole('button', { name: '打开项目并请求只读初始化' })).toHaveProperty('disabled', true);
  fireEvent.change(screen.getByLabelText(/实际停止检查记录/), { target: { value: '旧进程已检查' } });
  view.rerender(<ProjectInitialization snapshot={{ ...snapshot, initialization: {
    ...current, operation_version: current.operation_version + 1,
  } }} client={client} refresh={refresh} />);
  fireEvent.click(screen.getByRole('button', { name: '登记 Owner 停止核验' }));
  expect(write).not.toHaveBeenCalled();
  expect(screen.getByRole('alert').textContent).toContain('分析执行已变化');
});
