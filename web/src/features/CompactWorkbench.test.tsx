import { fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { App } from '../App';
import { ApiClient } from '../api/client';
import { LocalClient } from '../api/localClient';
import { centerSnapshot, connectionInfo } from '../test/connectionInfo';
import { TOKEN_KEY } from './Login';

afterEach(() => { sessionStorage.clear(); window.history.replaceState(null, '', '/'); vi.restoreAllMocks(); });
function setup() {
  vi.spyOn(LocalClient.prototype, 'info').mockResolvedValue({ mode: 'center' });
  vi.spyOn(ApiClient.prototype, 'snapshot').mockResolvedValue(centerSnapshot);
  vi.spyOn(ApiClient.prototype, 'read').mockResolvedValue(connectionInfo);
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {});
  render(<App />);
}
it.each([false, true])('keeps the compact login session choice %s and clears it on disconnect', async (remember) => {
  setup();
  fireEvent.change(await screen.findByLabelText('Owner token'), { target: { value: 'session-secret' } });
  expect(screen.queryByText(/明确目标/)).toBeNull();
  if (remember) fireEvent.click(screen.getByLabelText('本次会话记住'));
  fireEvent.click(screen.getByRole('button', { name: '连接工作台' }));
  await screen.findByRole('heading', { name: centerSnapshot.project!.name });
  expect(sessionStorage.getItem(TOKEN_KEY)).toBe(remember ? 'session-secret' : null);
  const footer = screen.getByRole('contentinfo');
  expect(within(footer).getByText(/中心已连接/)).toBeTruthy();
  expect(screen.getAllByRole('button', { name: '刷新中心状态' })).toHaveLength(1);
  fireEvent.click(within(footer).getByRole('button', { name: '断开工作台' }));
  expect(sessionStorage.getItem(TOKEN_KEY)).toBeNull();
  expect(screen.getByLabelText('Owner token')).toHaveProperty('value', '');
});
it('offers three primary settings segments and four additional management routes', async () => {
  sessionStorage.setItem(TOKEN_KEY, 'owner');
  window.history.replaceState(null, '', '/#settings');
  setup();
  await screen.findByRole('heading', { name: '项目设置' });
  expect(within(screen.getByRole('navigation', { name: '管理与设置' })).getAllByRole('button').map((button) => button.textContent)).toEqual(['项目', '系统', '接入']);
  const links = within(screen.getByRole('navigation', { name: '更多项目设置' }));
  expect(links.getAllByRole('button')).toHaveLength(4);
  for (const [label, hash] of [['项目初始化', 'initialization'], ['Leader 规划与咨询', 'planning'], ['运行与恢复', 'runs'], ['审计记录', 'audit']]) {
    fireEvent.click(screen.getByRole('button', { name: label }));
    expect(window.location.hash).toBe(`#${hash}`);
    expect(screen.getByRole('button', { name: '设置' }).getAttribute('aria-current')).toBe('page');
    fireEvent.click(screen.getByRole('button', { name: '返回设置' }));
  }
});
it('keeps connection failures and stale snapshot visible above the current page', async () => {
  sessionStorage.setItem(TOKEN_KEY, 'owner');
  setup();
  await screen.findByRole('heading', { name: centerSnapshot.project!.name });
  vi.mocked(ApiClient.prototype.snapshot).mockRejectedValue(new Error('中心暂不可达'));
  fireEvent.click(screen.getByRole('button', { name: '刷新中心状态' }));
  await screen.findByText('中心暂不可达 当前显示上次成功取得的状态。');
  expect(screen.getByText(/中心连接异常/).closest('.workspace-status')).toBeTruthy();
  expect(screen.getByRole('heading', { name: centerSnapshot.project!.name })).toBeTruthy();
  expect(within(screen.getByRole('contentinfo')).queryByText(/中心已连接/)).toBeNull();
  expect(screen.getAllByRole('button', { name: '刷新中心状态' })).toHaveLength(1);
});
