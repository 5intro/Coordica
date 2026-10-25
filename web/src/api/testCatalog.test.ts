import { expect, it } from 'vitest';
import { isCoverage } from './testCatalog';

it('rejects green claims without nonempty automated case evidence', () => {
  const coverage = { requirement: { id: 'req', title: 'payment', version: 1 },
    plan: { requirement_id: 'req', revision: 1, version: 1, criteria: [] },
    commit: 'a'.repeat(40), digest: 'b'.repeat(64), current: true, verified: true, criteria: [], gaps: [] };
  expect(isCoverage(coverage)).toBe(false);
  const item = { criterion: { id: 'once', title: 'pay once', method: 'automated' }, state: 'passed', cases: [] };
  expect(isCoverage({ ...coverage, criteria: [item] })).toBe(false);
  const evidence = { case: { id: 'case', version: 1, name: 'one payment' }, state: 'passed', run_id: 'run', platform: 'darwin' };
  expect(isCoverage({ ...coverage, criteria: [{ ...item, cases: [evidence] }] })).toBe(true);
  expect(isCoverage({ ...coverage, current: false, criteria: [{ ...item, cases: [evidence] }] })).toBe(false);
  expect(isCoverage({ ...coverage, criteria: [{ ...item, cases: [{ ...evidence, state: 'unreported' }] }] })).toBe(false);
});
