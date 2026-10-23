import { expect, it } from 'vitest';
import { readyInitialization } from '../test/initialization';
import { initializationReply } from './initialization';

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
