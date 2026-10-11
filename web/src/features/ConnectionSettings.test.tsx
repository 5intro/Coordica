import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiClient } from '../api/client';
import { connectionInfo, centerProject } from '../test/connectionInfo';
import { ConnectionSettings } from './ConnectionSettings';

afterEach(() => vi.unstubAllGlobals());

function show(client: ApiClient, info = connectionInfo) {
  return render(<ConnectionSettings client={client} info={info} error="" loading={false} reload={vi.fn()} project={centerProject} openPolicies={vi.fn()} />);
}

function fillNode() { fireEvent.change(screen.getByLabelText('接入节点名称'), { target: { value: 'Windows-dsh' } }); }

describe('center connection settings', () => {
  it('creates one pairing and copies an initialization instruction without owner credentials or center paths', async () => {
    const clipboard = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal('navigator', { clipboard: { writeText: clipboard } });
    const client = new ApiClient('private-owner-token');
    const write = vi.spyOn(client, 'write').mockResolvedValue({ code: 'pair_one', expires_at: '2030-01-01T00:00:00Z' });
    show(client);
    fillNode();
    fireEvent.click(screen.getByRole('button', { name: '生成并复制接入配置' }));
    await screen.findByRole('status');
    expect(write).toHaveBeenCalledTimes(1);
    expect(write).toHaveBeenCalledWith('/pairings', { name: 'Windows-dsh' }, expect.any(String));
    expect(clipboard).toHaveBeenCalledTimes(1);
    const value = clipboard.mock.calls[0][0] as string;
    expect(value).toContain('pair_one');
    expect(value).toContain(connectionInfo.runner_url);
    expect(value).toContain(connectionInfo.certificate_pem.replace(/\n/g, '\\n'));
    expect(value).not.toContain('private-');
    expect(value).not.toContain('data_dir');
    expect(screen.getByLabelText('一次性接入指令')).toHaveProperty('value', value);
    expect(screen.getByText('tools/**')).toBeTruthy();
    expect(screen.getByText('regression')).toBeTruthy();
  });

  it('keeps the receipt after clipboard failure and retries copying without issuing another pairing', async () => {
    const clipboard = vi.fn().mockRejectedValueOnce(new Error('clipboard denied')).mockResolvedValueOnce(undefined);
    vi.stubGlobal('navigator', { clipboard: { writeText: clipboard } });
    const storage = vi.spyOn(Storage.prototype, 'setItem');
    const client = new ApiClient('private-owner-token');
    const write = vi.spyOn(client, 'write').mockResolvedValue({ code: 'pair_one', expires_at: '2030-01-01T00:00:00Z' });
    show(client);
    fillNode();
    fireEvent.click(screen.getByRole('button', { name: '生成并复制接入配置' }));
    await screen.findByRole('alert');
    const value = (screen.getByLabelText('一次性接入指令') as HTMLTextAreaElement).value;
    expect(value).toContain('pair_one');
    fireEvent.click(screen.getByRole('button', { name: '复制已有接入配置' }));
    await screen.findByRole('status');
    expect(write).toHaveBeenCalledTimes(1);
    expect(clipboard).toHaveBeenLastCalledWith(value);
    expect(storage).not.toHaveBeenCalled();
  });

  it('retains the node name after a failed request and lets the user retry', async () => {
    vi.stubGlobal('navigator', { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } });
    const client = new ApiClient('owner');
    const write = vi.spyOn(client, 'write').mockRejectedValueOnce(new Error('中心暂时离线'))
      .mockResolvedValueOnce({ code: 'pair_retry', expires_at: '2030-01-01T00:00:00Z' });
    show(client);
    fillNode();
    fireEvent.click(screen.getByRole('button', { name: '生成并复制接入配置' }));
    await screen.findByText('中心暂时离线');
    expect(screen.queryByLabelText('一次性接入指令')).toBeNull();
    expect(screen.getByLabelText('接入节点名称')).toHaveProperty('value', 'Windows-dsh');
    fireEvent.click(screen.getByRole('button', { name: '生成并复制接入配置' }));
    await screen.findByRole('status');
    expect(write).toHaveBeenCalledTimes(2);
    expect(write.mock.calls[0][2]).toBe(write.mock.calls[1][2]);
  });

  it('reuses the pairing key when a JSON receipt is incomplete', async () => {
    vi.stubGlobal('navigator', { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } });
    const fetch = vi.fn().mockResolvedValueOnce(new Response('{}'))
      .mockResolvedValueOnce(new Response(JSON.stringify({ code: 'pair_receipt', expires_at: '2030-01-01T00:00:00Z' })));
    vi.stubGlobal('fetch', fetch);
    show(new ApiClient('owner'));
    fillNode();
    fireEvent.click(screen.getByRole('button', { name: '生成并复制接入配置' }));
    await screen.findByText('中心没有返回有效的一次性配对回执，请刷新后重试。');
    fireEvent.click(screen.getByRole('button', { name: '生成并复制接入配置' }));
    await screen.findByRole('status');
    const first = new Headers(fetch.mock.calls[0][1].headers).get('Idempotency-Key');
    const second = new Headers(fetch.mock.calls[1][1].headers).get('Idempotency-Key');
    expect(first).toBeTruthy();
    expect(second).toBe(first);
  });

  it('requires explicit regeneration once a pairing has expired', async () => {
    vi.stubGlobal('navigator', { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } });
    const client = new ApiClient('owner');
    const write = vi.spyOn(client, 'write').mockResolvedValueOnce({ code: 'pair_old', expires_at: '2020-01-01T00:00:00Z' })
      .mockResolvedValueOnce({ code: 'pair_new', expires_at: '2030-01-01T00:00:00Z' });
    show(client);
    fillNode();
    fireEvent.click(screen.getByRole('button', { name: '生成并复制接入配置' }));
    await screen.findByText('接入配置已到期，请重新生成。');
    expect(screen.getByRole('button', { name: '复制已有接入配置' })).toHaveProperty('disabled', true);
    fireEvent.click(screen.getByRole('button', { name: '重新生成并复制接入配置' }));
    await waitFor(() => expect((screen.getByLabelText('一次性接入指令') as HTMLTextAreaElement).value).toContain('pair_new'));
    expect(write.mock.calls[0][2]).not.toBe(write.mock.calls[1][2]);
  });

  it('disables configuration generation for loopback or wildcard center endpoints', async () => {
    const client = new ApiClient('owner');
    const write = vi.spyOn(client, 'write');
    const rendered = show(client, { ...connectionInfo, runner_url: 'http://127.0.0.1:8080' });
    fillNode();
    expect(screen.getByRole('button', { name: '生成并复制接入配置' })).toHaveProperty('disabled', true);
    rendered.rerender(<ConnectionSettings client={client} info={{ ...connectionInfo, runner_url: 'https://0.0.0.0:8443' }} error="" loading={false} reload={vi.fn()} openPolicies={vi.fn()} />);
    fireEvent.click(screen.getByRole('button', { name: '生成并复制接入配置' }));
    await waitFor(() => expect(write).not.toHaveBeenCalled());
  });
});
