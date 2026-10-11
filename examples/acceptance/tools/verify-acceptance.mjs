// This verifier belongs to the owner. Task scope excludes tools/**.
import assert from 'node:assert/strict';
import { greeting } from '../src/greeting.mjs';

for (const [input, expected] of [
  [undefined, 'Hello, guest!'],
  ['', 'Hello, guest!'],
  [' \t\n ', 'Hello, guest!'],
  ['  Ada  Lovelace\t', 'Hello, Ada Lovelace!'],
  ['\t李\u3000小龙\n', 'Hello, 李 小龙!'],
  [' pi ', 'Hello, pi!'],
]) assert.equal(greeting(input), expected);
for (const input of [null, 42, {}, []]) assert.throws(() => greeting(input), TypeError);
console.log('Independent acceptance passed.');
