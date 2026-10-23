import { expect, it } from 'vitest';
import { preparationSnapshot } from '../test/preparation';
import { validPolicyReceipt } from './policyReceipt';

it('accepts only a complete policy receipt bound to the original project and version', () => {
  const project = preparationSnapshot.project!;
  const request = { expected_version: project.version, checks: project.checks, protected_paths: project.protected_paths };
  const receipt = { ...project, version: project.version + 1, policy_version: project.policy_version + 1 };
  expect(validPolicyReceipt(receipt, project.id, request, project.policy_version)).toBe(true);
  for (const bad of [
    {}, { ...receipt, id: 'other' }, { ...receipt, version: project.version },
    { ...receipt, checks: [] }, { ...receipt, protected_paths: null },
    { ...receipt, policy_version: undefined }, { ...receipt, reserved_seconds: undefined },
    { ...receipt, policy_version: project.policy_version },
    { ...receipt, checks: [{ ...receipt.checks![0], command: 'unrelated' }] },
    { ...receipt, protected_paths: [] },
  ]) expect(validPolicyReceipt(bad, project.id, request, project.policy_version)).toBe(false);
});
