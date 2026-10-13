import { describe, expect, it } from 'vitest';
import { readyInitialization } from '../test/initialization';
import { validInitialization } from './initialization';
describe('initialization runtime contract', () => {
  it('rejects incomplete result, unknown state and forged confirmation digest', () => {
    expect(validInitialization(readyInitialization)).toBe(true);
    expect(validInitialization({ ...readyInitialization, status: 'ready' })).toBe(false);
    expect(validInitialization({ ...readyInitialization, digest: 'forged' })).toBe(false);
    expect(validInitialization({ ...readyInitialization, result: { ready: true } })).toBe(false);
    expect(validInitialization({ ...readyInitialization, covered_paths: null })).toBe(false);
    expect(validInitialization({ ...readyInitialization, feedback: [null] })).toBe(false);
    expect(validInitialization({ ...readyInitialization, result: { ...readyInitialization.result, facts: [null] } })).toBe(false);
    expect(validInitialization({ ...readyInitialization, result: { ...readyInitialization.result, baseline: { ...readyInitialization.result!.baseline, checks: [{ ...readyInitialization.result!.baseline.checks![0], args: [123] }] } } })).toBe(false);
    expect(validInitialization({ ...readyInitialization, result: { ...readyInitialization.result, baseline: { ...readyInitialization.result!.baseline, checks: [{ ...readyInitialization.result!.baseline.checks![0], timeout_seconds: 600 }] } } })).toBe(true);
  });
});
