import test from 'node:test';
import assert from 'node:assert/strict';
import { greeting } from '../src/greeting.mjs';

test('greets a name', () => assert.equal(greeting('Ada'), 'Hello, Ada!'));
test('greets an omitted or empty name', () => {
  assert.equal(greeting(), 'Hello, guest!');
  assert.equal(greeting(''), 'Hello, guest!');
});
