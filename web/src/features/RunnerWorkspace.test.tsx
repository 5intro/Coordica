import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
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
    fireEvent.click(screen.getByText('同机中心 · 需 owner 登录'));
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

  it('keeps local navigation in the page and exposes execution IDs only in technical details', async () => {
    const client = new LocalClient();
    vi.spyOn(client, 'snapshot').mockResolvedValue(nodeSnapshot);
    const { container } = render(<RunnerWorkspace client={client} />);
    await screen.findByText('规范化问候姓名');
    const main = screen.getByRole('main');
    expect(main.contains(screen.getByRole('navigation', { name: '本机工作台页面' }))).toBe(true);
    expect(container.querySelector('.sidebar, .topbar, .nav-item')).toBeNull();
    expect(screen.getByRole('button', { name: /本机活动/ }).getAttribute('aria-pressed')).toBe('true');
    const technical = screen.getByText(localRun.id).closest('details')!;
    expect(technical.open).toBe(false);
    fireEvent.click(technical.querySelector('summary')!);
    expect(technical.open).toBe(true);
    fireEvent.click(screen.getByRole('button', { name: '提交需求' }));
    expect(screen.getByRole('button', { name: '提交需求' }).getAttribute('aria-pressed')).toBe('true');
    expect(screen.getByLabelText('需求标题')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /本机活动/ }));
    expect(screen.getByText('规范化问候姓名')).toBeTruthy();
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

  it('collapses all tool diagnostics while keeping executions, pause and connection errors visible', async () => {
    const client = new LocalClient();
    const capabilities = Array.from({ length: 6 }, (_, index) => ({
      ...nodeSnapshot.node.capabilities![0], tool: `tool-${index}`, available: index < 2,
      version: `version-${index}`, protocol: `protocol-${index}`,
      structured: index % 2 === 0, cancel: index % 2 === 0, review: index % 2 === 0,
      read_only: index % 2 !== 0, resume: index % 2 !== 0,
      detail: `tool-${index}: version detected; protocol/capabilities declared; raw diagnostic`,
    }));
    vi.spyOn(client, 'snapshot').mockResolvedValueOnce({
      ...nodeSnapshot, project_paused: true, node: { ...nodeSnapshot.node, capabilities },
      runs: [localRun, { ...localRun, id: 'cancel-run', state: 'cancel_requested' },
        { ...localRun, id: 'lost-run', state: 'lost' },
        { ...localRun, id: 'failed-run', state: 'failed', quiescent: false }],
    }).mockRejectedValueOnce(new Error('中心离线'));
    const { container } = render(<RunnerWorkspace client={client} />);
    const summary = await screen.findByText('工具能力与诊断 · 2 项可用');
    const details = summary.closest('details')!;
    expect(details.open).toBe(false);
    // Model native details visibility: closed groups contribute only their summary text.
    const visibleText = () => {
      const copy = container.cloneNode(true) as HTMLElement;
      copy.querySelectorAll('details:not([open])').forEach((group) => {
        group.replaceChildren(group.querySelector('summary')!);
      });
      return copy.textContent!;
    };
    for (const capability of capabilities) {
      expect(visibleText()).not.toContain(capability.detail);
      expect(visibleText()).not.toContain(capability.version);
    }
    expect(screen.getAllByRole('row')).toHaveLength(5);
    expect(visibleText()).toContain('规范化问候姓名');
    expect(visibleText()).toContain('未确认静止');
    expect(visibleText()).toContain('失联 · 未确认停止');
    expect(visibleText()).toContain('中心项目已暂停');
    expect(visibleText()).toContain('Coordica · 本机Runner');
    fireEvent.click(screen.getByRole('button', { name: '刷新 ↻' }));
    await screen.findByRole('alert');
    expect(visibleText()).toContain('中心离线');
    expect(visibleText()).toContain('上次成功取得的状态');
    expect(screen.getAllByRole('row')).toHaveLength(5);
    expect(details.open).toBe(false);
    fireEvent.click(summary);
    expect(details.open).toBe(true);
    for (const capability of capabilities) {
      expect(visibleText()).toContain(capability.detail);
      expect(visibleText()).toContain(capability.version);
      expect(visibleText()).toContain(`协议：${capability.protocol}`);
    }
    expect(within(details).getAllByText('探测可用')).toHaveLength(2);
    expect(within(details).getAllByText('探测不可用')).toHaveLength(4);
    expect(within(details).getAllByText('结构化输出：支持 · 取消：支持 · 独立评审：支持')).toHaveLength(3);
    expect(within(details).getAllByText('结构化输出：未声明 · 取消：未声明 · 独立评审：未声明')).toHaveLength(3);
    expect(within(details).getAllByText('只读：支持 · 恢复：支持')).toHaveLength(3);
    expect(within(details).getAllByText('只读：未声明 · 恢复：未声明')).toHaveLength(3);
  });

  it('keeps empty and unavailable capability reports truthful without empty version placeholders', async () => {
    const client = new LocalClient();
    vi.spyOn(client, 'snapshot').mockResolvedValueOnce({
      ...nodeSnapshot, node: { ...nodeSnapshot.node, capabilities: [] },
    }).mockResolvedValueOnce({
      ...nodeSnapshot, node: { ...nodeSnapshot.node, capabilities: [{
        ...nodeSnapshot.node.capabilities![0], available: false, version: '', protocol: '',
        detail: 'probe failed: executable missing',
      }] },
    });
    render(<RunnerWorkspace client={client} />);
    const summary = await screen.findByText('工具能力与诊断 · 0 项可用');
    const details = summary.closest('details')!;
    expect(details.open).toBe(false);
    fireEvent.click(summary);
    expect(within(details).getByText('尚未报告 Agent 工具能力。')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '刷新 ↻' }));
    await screen.findByText('probe failed: executable missing');
    expect(within(details).getByText('探测不可用')).toBeTruthy();
    expect(within(details).queryByText('版本未知')).toBeNull();
    expect(within(details).queryByText(/协议未报告/)).toBeNull();
    expect(screen.getByText('规范化问候姓名')).toBeTruthy();
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
