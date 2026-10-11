import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { App } from './App';
import { LocalClient } from './api/localClient';
import { ApiClient } from './api/client';
import { TOKEN_KEY } from './features/Login';
import { nodeSnapshot } from './test/nodeSnapshot';
import { centerSnapshot, connectionInfo } from './test/connectionInfo';

afterEach(() => { sessionStorage.clear(); window.history.replaceState(null, '', '/'); });

describe('local page selection', () => {
  it('returns to overview when browser back restores the initial URL without a hash', async () => {
    window.history.replaceState(null, '', '/');
    sessionStorage.setItem(TOKEN_KEY, 'owner-session');
    vi.spyOn(window, 'scrollTo').mockImplementation(() => {});
    vi.spyOn(LocalClient.prototype, 'info').mockResolvedValue({ mode: 'center' });
    vi.spyOn(ApiClient.prototype, 'snapshot').mockResolvedValue(centerSnapshot);
    vi.spyOn(ApiClient.prototype, 'read').mockResolvedValue(connectionInfo);
    render(<App />);
    await screen.findByRole('heading', { level: 1, name: centerSnapshot.project!.name });
    fireEvent.click(screen.getByRole('button', { name: /任务池/ }));
    await screen.findByRole('heading', { level: 1, name: '任务池' });
    expect(window.location.hash).toBe('#tasks');
    window.history.back();
    await screen.findByRole('heading', { level: 1, name: centerSnapshot.project!.name });
    expect(window.location.hash).toBe('');
  });

  it('preserves a requested center page through owner login and responds to hash navigation', async () => {
    window.history.replaceState(null, '', '/#tasks');
    vi.spyOn(window, 'scrollTo').mockImplementation(() => {});
    vi.spyOn(LocalClient.prototype, 'info').mockResolvedValue({ mode: 'center' });
    vi.spyOn(ApiClient.prototype, 'snapshot').mockResolvedValue(centerSnapshot);
    vi.spyOn(ApiClient.prototype, 'read').mockResolvedValue(connectionInfo);
    render(<App />);
    const token = await screen.findByLabelText('Owner token');
    expect(screen.queryByRole('heading', { name: '任务池' })).toBeNull();
    fireEvent.change(token, { target: { value: 'owner-login-token' } });
    fireEvent.click(screen.getByRole('button', { name: /连接工作台/ }));
    await screen.findByRole('heading', { name: '任务池' });
    window.history.replaceState(null, '', '/#requirements');
    fireEvent(window, new HashChangeEvent('hashchange'));
    await screen.findByRole('heading', { name: '需求池' });
    fireEvent.click(screen.getByRole('button', { name: /中心配置与接入/ }));
    await screen.findByRole('heading', { name: '中心配置与接入' });
    expect(window.location.hash).toBe('#connection');
    expect(screen.getAllByRole('link', { name: '本机 Runner 默认入口 ↗' })[0].getAttribute('href')).toBe('http://127.0.0.1:8081/');
  });

  it('opens connection settings before a project has been created', async () => {
    window.history.replaceState(null, '', '/#connection');
    sessionStorage.setItem(TOKEN_KEY, 'owner-session');
    vi.spyOn(LocalClient.prototype, 'info').mockResolvedValue({ mode: 'center' });
    vi.spyOn(ApiClient.prototype, 'snapshot').mockResolvedValue({ ...centerSnapshot, project: null });
    vi.spyOn(ApiClient.prototype, 'read').mockResolvedValue(connectionInfo);
    render(<App />);
    await screen.findByText('当前中心部署');
    expect(screen.getByText('中心尚未创建项目。可以先配对节点，再从项目概览完成项目设置。')).toBeTruthy();
    expect(screen.getByRole('button', { name: '生成并复制接入配置' })).toBeTruthy();
  });

  it('opens the Runner page without using a stored center owner credential', async () => {
    sessionStorage.setItem(TOKEN_KEY, 'owner-session');
    vi.spyOn(LocalClient.prototype, 'info').mockResolvedValue({ mode: 'runner' });
    vi.spyOn(LocalClient.prototype, 'snapshot').mockResolvedValue(nodeSnapshot);
    const ownerSnapshot = vi.spyOn(ApiClient.prototype, 'snapshot');
    render(<App />);
    await screen.findByText('本机 Agent 在做什么');
    expect(ownerSnapshot).not.toHaveBeenCalled();
    expect(screen.queryByLabelText('Owner token')).toBeNull();
    expect(screen.queryByText('集成候选')).toBeNull();
  });

  it('keeps discovery failure retryable without selecting the owner console', async () => {
    vi.spyOn(LocalClient.prototype, 'info').mockRejectedValueOnce(new Error('本机服务连接失败')).mockResolvedValueOnce({ mode: 'center' });
    render(<App />);
    await screen.findByText('本机服务连接失败');
    expect(screen.queryByLabelText('Owner token')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: '重新连接' }));
    await screen.findByLabelText('Owner token');
  });
});
