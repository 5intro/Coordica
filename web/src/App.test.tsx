import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { App } from './App';
import { LocalClient } from './api/localClient';
import { ApiClient } from './api/client';
import { TOKEN_KEY } from './features/Login';
import { nodeSnapshot } from './test/nodeSnapshot';

afterEach(() => sessionStorage.clear());

describe('local page selection', () => {
  it('opens the Runner page without using a stored center owner credential', async () => {
    sessionStorage.setItem(TOKEN_KEY, 'owner-session');
    vi.spyOn(LocalClient.prototype, 'info').mockResolvedValue('runner');
    vi.spyOn(LocalClient.prototype, 'snapshot').mockResolvedValue(nodeSnapshot);
    const ownerSnapshot = vi.spyOn(ApiClient.prototype, 'snapshot');
    render(<App />);
    await screen.findByText('本机 Agent 在做什么');
    expect(ownerSnapshot).not.toHaveBeenCalled();
    expect(screen.queryByLabelText('Owner token')).toBeNull();
    expect(screen.queryByText('集成候选')).toBeNull();
  });

  it('keeps discovery failure retryable without selecting the owner console', async () => {
    vi.spyOn(LocalClient.prototype, 'info').mockRejectedValueOnce(new Error('本机服务连接失败')).mockResolvedValueOnce('center');
    render(<App />);
    await screen.findByText('本机服务连接失败');
    expect(screen.queryByLabelText('Owner token')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: '重新连接' }));
    await screen.findByLabelText('Owner token');
  });
});
