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
