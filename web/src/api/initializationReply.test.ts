import { afterEach, describe, expect, it, vi } from 'vitest';
import { readyInitialization } from '../test/initialization';
import { ApiClient } from './client';
import { initializationReply, validInitialization } from './initialization';

const wireInitialization = { ...readyInitialization, kind: 'new', omissions: null };
const stateWith = (initialization: unknown) => ({
  api_version: 'v1', project: null, requirements: [], tasks: [], runs: [],
  candidates: [], nodes: [], events: [], initialization,
});
const invalidReplies = [
  { ...wireInitialization, omissions: undefined },
  { ...wireInitialization, omissions: 'missing' },
  { ...wireInitialization, omissions: [null] },
  { ...wireInitialization, feedback: null },
  { ...wireInitialization, covered_paths: null },
  { ...wireInitialization, digest: 'forged' },
  { ...wireInitialization, version: readyInitialization.version + 1 },
  { ...wireInitialization, result: { ready: true } },
];

afterEach(() => vi.unstubAllGlobals());

describe('initialization transport compatibility', () => {
  it('normalizes only known null omissions without mutating input or confirmation identity', () => {
    const input = Object.freeze({ ...wireInitialization });
    const before = JSON.stringify(input);
    const result = initializationReply(input);
    expect(result).toEqual({ ...input, omissions: [] });
    expect(result).not.toBe(input);
    expect(JSON.stringify(input)).toBe(before);
    expect(result?.digest).toBe(input.digest);
    expect(result?.version).toBe(input.version);
    expect(result?.operation_version).toBe(input.operation_version);
    expect(result?.result).toBe(input.result);
    expect(validInitialization(input)).toBe(false);
    expect(validInitialization(result)).toBe(true);
  });

  it.each([{ omissions: [] }, { omissions: ['excluded private files'] }])('preserves already valid omissions $omissions', ({ omissions }) => {
    const input = { ...readyInitialization, omissions };
    expect(initializationReply(input)).toBe(input);
  });

  it.each(invalidReplies)('does not repair malformed initialization %#', (input) => {
    expect(initializationReply(input)).toBeUndefined();
  });

  describe.each(['state', 'initialization'] as const)('%s API entry point', (entry) => {
    const read = async (client: ApiClient) => entry === 'state' ? (await client.snapshot()).initialization : client.initialization();
    const respond = (value: unknown) => {
      const fetcher = vi.fn().mockImplementation(async () => new Response(JSON.stringify(entry === 'state' ? stateWith(value) : value), { status: 200 }));
      vi.stubGlobal('fetch', fetcher);
      return fetcher;
    };

    it.each([{ omissions: null }, { omissions: [] }, { omissions: ['excluded private files'] }])('accepts the known wire form %#', async ({ omissions }) => {
      const value = { ...wireInitialization, omissions };
      const fetcher = respond(value);
      const result = await read(new ApiClient('owner-token'));
      expect(result).toEqual({ ...value, omissions: omissions ?? [] });
      expect(result?.digest).toBe(value.digest);
      expect(result?.version).toBe(value.version);
      expect(fetcher).toHaveBeenCalledWith(`/api/v1/${entry}`, expect.objectContaining({ headers: expect.objectContaining({ Authorization: 'Bearer owner-token' }) }));
    });

    it.each(invalidReplies)('rejects malformed wire data %#', async (value) => {
      respond(value);
      await expect(read(new ApiClient('owner-token'))).rejects.toMatchObject({ code: 'incompatible_response' });
    });
  });

  it('keeps absent initialization valid in a state snapshot', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify(stateWith(null)))));
    await expect(new ApiClient('owner-token').snapshot()).resolves.toMatchObject({ initialization: null });
  });
});

it('normalizes the known legacy empty omissions representation without changing its input or digest', () => {
  const old = { ...readyInitialization, omissions: null };
  const value = initializationReply(old);
  expect(value?.omissions).toEqual([]);
  expect(value?.digest).toBe(old.digest);
  expect(value?.version).toBe(old.version);
  expect(old.omissions).toBeNull();
});

it('does not accept missing or malformed contract fields as a successful answer receipt', () => {
  expect(initializationReply({})).toBeUndefined();
  expect(initializationReply({ ...readyInitialization, omissions: [42] })).toBeUndefined();
  expect(initializationReply({ ...readyInitialization, omissions: null, feedback: null })).toBeUndefined();
});

it.each(['operation_id', 'operation_generation', 'operation_version', 'operation_state'])(
  'rejects an analyzing receipt missing %s, including one retaining the previous result', (field) => {
    for (const result of [null, readyInitialization.result]) {
      const value: Record<string, unknown> = { ...readyInitialization, status: 'analyzing', result };
      delete value[field];
      expect(initializationReply(value)).toBeUndefined();
    }
  },
);

it('requires real operation metadata after initialization starts but permits an untouched draft', () => {
  const analyzing = { ...readyInitialization, status: 'analyzing', result: null, operation_state: 'queued' };
  expect(initializationReply(analyzing)).toBeTruthy();
  for (const change of [
    { operation_id: '' }, { operation_generation: 0 }, { operation_version: -1 },
    { operation_generation: 1.5 }, { operation_state: 'imagined' },
  ]) expect(initializationReply({ ...analyzing, ...change })).toBeUndefined();
  const draft = { ...analyzing, status: 'draft', version: 0,
    operation_id: '', operation_generation: 0, operation_version: 0, operation_state: '' };
  expect(initializationReply(draft)).toBeTruthy();
});
