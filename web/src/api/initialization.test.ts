import { describe, expect, it } from 'vitest';
import { readyInitialization } from '../test/initialization';
import { validInitialization } from './initialization';
describe('initialization runtime contract', () => {
  it('requires the confirmed agreement to match the actual approved version and digest', () => {
    const confirmed = {
      ...readyInitialization, status: 'initialized', approved_version: readyInitialization.version,
      approved_digest: readyInitialization.digest, approved_baseline: readyInitialization.result!.baseline,
    };
    expect(validInitialization(confirmed)).toBe(true);
    expect(validInitialization({ ...confirmed, approved_version: 1 })).toBe(false);
    expect(validInitialization({ ...confirmed, approved_digest: 'd'.repeat(64) })).toBe(false);
    expect(validInitialization({ ...confirmed, approved_baseline: null })).toBe(false);
    expect(validInitialization({ ...confirmed, approved_baseline: {} })).toBe(false);
  });
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
