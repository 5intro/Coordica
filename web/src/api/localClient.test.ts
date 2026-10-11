import { afterEach, describe, expect, it, vi } from 'vitest';
import { LocalClient } from './localClient';
import { nodeSnapshot, localRun } from '../test/nodeSnapshot';

afterEach(() => vi.unstubAllGlobals());

describe('local Runner client', () => {
  it('accepts mode-only metadata and the optional same-machine center link', async () => {
    const fetch = vi.fn().mockResolvedValueOnce(new Response(JSON.stringify({ mode: 'runner' })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ mode: 'runner', center_console_url: 'http://127.0.0.1:8080' })));
    vi.stubGlobal('fetch', fetch);
    const client = new LocalClient();
    expect(await client.info()).toEqual({ mode: 'runner' });
    expect(await client.info()).toEqual({ mode: 'runner', center_console_url: 'http://127.0.0.1:8080' });
    expect(new Headers(fetch.mock.calls[0][1].headers).has('Authorization')).toBe(false);
  });

  it('uses only the node-scoped local endpoint without sending a browser credential', async () => {
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(nodeSnapshot)));
    vi.stubGlobal('fetch', fetch);
    const snapshot = await new LocalClient().snapshot();
    expect(snapshot.node.id).toBe('node_local');
    expect(fetch.mock.calls[0][0]).toBe('/api/v1/local/state');
    expect(new Headers(fetch.mock.calls[0][1].headers).has('Authorization')).toBe(false);
  });

  it('rejects another node’s work instead of displaying an expanded center snapshot', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ ...nodeSnapshot,
      runs: [localRun, { ...localRun, id: 'run_other', node_id: 'node_other' }],
    }))));
    await expect(new LocalClient().snapshot()).rejects.toThrow('不属于此节点');
  });

  it('reuses the idempotency key after an uncertain submission receipt', async () => {
    const requirement = { id: 'req_local', version: 1, title: '新增搜索', body: '支持按名称搜索', source_node_id: 'node_local', created_at: '' };
    const fetch = vi.fn().mockResolvedValueOnce(new Response('connection interrupted'))
      .mockResolvedValueOnce(new Response(JSON.stringify(requirement)));
    vi.stubGlobal('fetch', fetch);
    const client = new LocalClient();
    const proposal = { title: requirement.title, body: requirement.body };
    await expect(client.submitRequirement(proposal)).rejects.toThrow('JSON');
    expect(await client.submitRequirement(proposal)).toEqual(requirement);
    const first = new Headers(fetch.mock.calls[0][1].headers);
    const second = new Headers(fetch.mock.calls[1][1].headers);
    expect(first.get('Idempotency-Key')).toBeTruthy();
    expect(first.get('Idempotency-Key')).toBe(second.get('Idempotency-Key'));
    expect(first.get('X-Coordica-Local')).toBe('1');
    expect(second.has('Authorization')).toBe(false);
  });
});
