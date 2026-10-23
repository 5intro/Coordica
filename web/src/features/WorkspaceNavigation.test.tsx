import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { SettingsNavigation, WorkspaceDock } from './WorkspaceNavigation';

it('gives Leader an independent Dock destination without settings navigation', () => {
  const navigate = vi.fn();
  render(<><SettingsNavigation page="planning" navigate={navigate} /><WorkspaceDock page="planning" navigate={navigate} /></>);
  const dock = within(screen.getByRole('navigation', { name: '工作台页面' }));
  expect(dock.getByRole('button', { name: 'Leader 活动' }).getAttribute('aria-current')).toBe('page');
  expect(dock.getByRole('button', { name: '设置' }).getAttribute('aria-current')).toBeNull();
  expect(screen.queryByRole('navigation', { name: '管理与设置' })).toBeNull();
  expect(screen.queryByRole('button', { name: '返回设置' })).toBeNull();
  fireEvent.click(dock.getByRole('button', { name: 'Leader 活动' }));
  expect(navigate).toHaveBeenCalledWith('planning');
});

it('keeps project preparation and recovery in settings, excluding Leader', () => {
  render(<SettingsNavigation page="settings" navigate={vi.fn()} />);
  const menu = within(screen.getByRole('navigation', { name: '更多项目设置' }));
  expect(menu.getByRole('button', { name: '项目准备' })).toBeTruthy();
  expect(menu.queryByRole('button', { name: /Leader/ })).toBeNull();
});
