// @vitest-environment node
import { createServer } from 'node:http';
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest';
import { ApiClient } from './client';
import { readyInitialization } from '../test/initialization';

// Real HTTP transport coverage of both public ApiClient read paths. This fixture
// reproduces Go's nil-slice JSON, without changing the server's stored digest.
const initialization = { ...readyInitialization, kind: 'new', omissions: null };
const requests = [];
const server = createServer((request, response) => {
  requests.push(request.url ?? '');
  response.setHeader('Content-Type', 'application/json');
  if (request.headers.authorization !== 'Bearer owner-fixture') {
    response.writeHead(401).end(JSON.stringify({ code: 'unauthorized' }));
    return;
  }
  const snapshot = {
    api_version: 'v1', project: null, requirements: [], tasks: [], runs: [],
    candidates: [], nodes: [], events: [], initialization,
  };
  response.end(JSON.stringify(request.url === '/api/v1/state' ? snapshot : initialization));
});

beforeAll(async () => {
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('HTTP fixture did not bind');
  const nativeFetch = globalThis.fetch;
  vi.stubGlobal('fetch', (path, init) => nativeFetch(new URL(path, `http://127.0.0.1:${address.port}`), init));
});
afterAll(async () => {
  vi.unstubAllGlobals();
  await new Promise((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
});

describe('initialization over HTTP', () => {
  it('reads null omissions through state and direct initialization without losing identity', async () => {
    const client = new ApiClient('owner-fixture');
    const snapshot = await client.snapshot();
    const direct = await client.initialization();
    expect(requests).toEqual(['/api/v1/state', '/api/v1/initialization']);
    expect(snapshot.initialization).toEqual({ ...initialization, omissions: [] });
    expect(direct).toEqual(snapshot.initialization);
    expect(direct.digest).toBe(initialization.digest);
    expect(direct.version).toBe(initialization.version);
    expect(initialization.omissions).toBeNull();
  });
});
