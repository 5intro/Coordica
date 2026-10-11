import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { LocalClient } from '../api/localClient';
import { nodeSnapshot, localRun } from '../test/nodeSnapshot';
import { RunnerWorkspace } from './RunnerWorkspace';

describe('local Runner workbench', () => {
  it('shows actual local execution and keeps the last state visibly stale when disconnected', async () => {
    const client = new LocalClient();
    vi.spyOn(client, 'snapshot').mockResolvedValueOnce(nodeSnapshot).mockRejectedValueOnce(new Error('中心离线'));
    render(<RunnerWorkspace client={client} />);
    await screen.findByText('规范化问候姓名');
    expect(screen.getByText('实现中')).toBeTruthy();
    expect(screen.getByText('12 秒')).toBeTruthy();
    expect(screen.queryByRole('button', { name: '撤销节点' })).toBeNull();
    expect(screen.queryByRole('button', { name: '批准任务' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: '刷新 ↻' }));
    await screen.findByRole('alert');
    expect(screen.getByRole('alert').textContent).toContain('上次成功取得的状态');
    expect(screen.getByText('规范化问候姓名')).toBeTruthy();
  });

  it('does not label a lost unconfirmed process as actively developing', async () => {
    const client = new LocalClient();
    vi.spyOn(client, 'snapshot').mockResolvedValue({ ...nodeSnapshot, runs: [{ ...localRun, state: 'lost' }] });
    render(<RunnerWorkspace client={client} />);
    await screen.findByText('失联 · 未确认停止');
    expect(screen.queryByText('实现中')).toBeNull();
  });

  it('preserves a failed requirement draft for retry and clears it only after a receipt', async () => {
    const client = new LocalClient();
    vi.spyOn(client, 'snapshot').mockResolvedValue(nodeSnapshot);
    const submit = vi.spyOn(client, 'submitRequirement').mockRejectedValueOnce(new Error('中心连接中断'))
      .mockResolvedValueOnce({ id: 'req_1', version: 1, title: '新增搜索', body: '按名称搜索', source_node_id: 'node_local', created_at: '' });
    render(<RunnerWorkspace client={client} />);
    await screen.findByText('规范化问候姓名');
    fireEvent.click(screen.getByRole('button', { name: /提交需求/ }));
    const title = screen.getByLabelText('需求标题') as HTMLInputElement;
    const body = screen.getByLabelText('原始需求与成功条件') as HTMLTextAreaElement;
    fireEvent.change(title, { target: { value: '新增搜索' } });
    fireEvent.change(body, { target: { value: '按名称搜索' } });
    fireEvent.click(screen.getByRole('button', { name: '提交到中心' }));
    await screen.findByText('中心连接中断');
    expect(title.value).toBe('新增搜索');
    expect(body.value).toBe('按名称搜索');
    fireEvent.click(screen.getByRole('button', { name: '提交到中心' }));
    await waitFor(() => expect(title.value).toBe(''));
    expect(submit).toHaveBeenCalledTimes(2);
    expect(submit).toHaveBeenLastCalledWith({ title: '新增搜索', body: '按名称搜索' });
    expect(screen.getByRole('status').textContent).toContain('等待形成获批任务');
  });
});
