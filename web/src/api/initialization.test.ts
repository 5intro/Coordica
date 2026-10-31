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
    expect(validInitialization({ ...confirmed, approved_baseline: {
      ...confirmed.approved_baseline, architecture: 'unrelated architecture',
    } })).toBe(false);
    expect(validInitialization({ ...confirmed, approved_baseline: Object.fromEntries(
      Object.entries(confirmed.approved_baseline).reverse(),
    ) })).toBe(true);
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
  it('binds required inputs and generated outputs to the confirmed agreement', () => {
    const baseline = structuredClone(readyInitialization.result!.baseline);
    baseline.checks![0].required_paths = ['entry.ts'];
    baseline.checks![0].output_paths = ['build/**'];
    const confirmed = { ...readyInitialization, status: 'initialized', approved_version: readyInitialization.version,
      approved_digest: readyInitialization.digest, approved_baseline: structuredClone(baseline),
      result: { ...readyInitialization.result!, baseline } };
    expect(validInitialization(confirmed)).toBe(true);
    for (const key of ['required_paths', 'output_paths'] as const) {
      const changed = structuredClone(confirmed);
      changed.result.baseline.checks![0][key] = ['unrelated'];
      expect(validInitialization(changed)).toBe(false);
    }
    expect(validInitialization({ ...confirmed, check_input_paths: [123] })).toBe(false);
  });
});
