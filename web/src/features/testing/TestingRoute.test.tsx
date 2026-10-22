import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { ApiClient } from '../../api/client';
import { centerSnapshot } from '../../test/connectionInfo';
import { Testing } from './TestingWorkspace';

afterEach(() => window.history.replaceState({}, '', '#'));
it.each(['测试条目', '需求验收与补测'])('leaves a report deep link for %s with one history entry', (name) => {
  const original = '#testing?run=historical-run&view=history&query=retained';
  window.history.replaceState({}, '', original);
  const client = new ApiClient('owner');
  vi.spyOn(client, 'read').mockResolvedValue({ items: [], next: '' });
  const push = vi.spyOn(window.history, 'pushState');
  render(<Testing snapshot={centerSnapshot} client={client} refresh={vi.fn()} />);
  const reportTab = screen.getByRole('button', { name: '套件与执行' });
  expect(reportTab.getAttribute('aria-pressed')).toBe('true');
  push.mockClear();
  fireEvent.click(screen.getByRole('button', { name }));
  expect(screen.getByRole('button', { name }).getAttribute('aria-pressed')).toBe('true');
  expect(window.location.hash).not.toContain('run=');
  expect(window.location.hash).toContain('query=retained');
  expect(push).toHaveBeenCalledTimes(1);
  act(() => {
    window.history.replaceState({}, '', original);
    window.dispatchEvent(new PopStateEvent('popstate'));
  });
  expect(reportTab.getAttribute('aria-pressed')).toBe('true');
});
