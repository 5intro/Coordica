import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { LocalClient } from '../api/localClient';
import { nodeSnapshot, localRun } from '../test/nodeSnapshot';
import { RunnerWorkspace } from './RunnerWorkspace';

describe('local Runner workbench', () => {
  it('offers center navigation only for a safe same-machine link and does not fetch owner state', async () => {
    const client = new LocalClient();
    vi.spyOn(client, 'snapshot').mockResolvedValue(nodeSnapshot);
    const rendered = render(<RunnerWorkspace client={client} centerConsoleURL="http://127.0.0.1:8080" />);
    await screen.findByText('规范化问候姓名');
    expect(screen.getByRole('link', { name: '中心任务池 ↗' }).getAttribute('href')).toBe('http://127.0.0.1:8080/#tasks');
    expect(screen.getByRole('link', { name: '中心需求池 ↗' }).getAttribute('href')).toBe('http://127.0.0.1:8080/#requirements');
    expect(screen.getByRole('link', { name: '中心配置与接入 ↗' }).getAttribute('href')).toBe('http://127.0.0.1:8080/#connection');
    expect(screen.getByRole('link', { name: '中心阻断管理 ↗' }).getAttribute('href')).toBe('http://127.0.0.1:8080/#blocks');
    expect(screen.getByRole('link', { name: '中心 Agents 管理 ↗' }).getAttribute('href')).toBe('http://127.0.0.1:8080/#agents');
    expect(screen.queryByLabelText('Owner token')).toBeNull();
    rendered.rerender(<RunnerWorkspace client={client} centerConsoleURL="https://evil.example" />);
    expect(screen.queryByRole('navigation', { name: '同机中心工作台' })).toBeNull();
    expect(screen.queryByRole('button', { name: '配对节点 ＋' })).toBeNull();
    expect(screen.queryByRole('button', { name: '撤销节点' })).toBeNull();
    rendered.rerender(<RunnerWorkspace client={client} />);
    expect(screen.queryByRole('link', { name: '中心任务池 ↗' })).toBeNull();
  });

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
