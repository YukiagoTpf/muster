import assert from 'node:assert/strict';
import { test } from 'node:test';
import { del, get, isProviderKey, renameKey, set, stringify, type JsonObject } from './projection.ts';

const original: JsonObject = JSON.parse('{"model":"opus","env":{"A":"1"},"permissions":{"allow":["Read"]},"hooks":{}}');

test('set keeps key order and leaves other values alone', () => {
  const d = set(original, ['env', 'A'], '2');
  assert.deepEqual(Object.keys(d), ['model', 'env', 'permissions', 'hooks']);
  assert.equal(get(d, ['env', 'A']), '2');
  assert.equal(d.permissions, original.permissions, 'an untouched branch is the same object');
  const added = set(original, ['theme'], 'dark');
  assert.deepEqual(Object.keys(added), ['model', 'env', 'permissions', 'hooks', 'theme']);
});

test('toggling a key on and back off restores the file exactly', () => {
  let d = set(original, ['sandbox', 'enabled'], true);
  assert.deepEqual(get(d, ['sandbox']), { enabled: true });
  d = del(d, ['sandbox', 'enabled'], original);
  assert.equal(stringify(d), stringify(original), 'the container the editor created is gone again');
});

test('a container the file had stays, empty', () => {
  const d = del(original, ['env', 'A'], original);
  assert.deepEqual(get(d, ['env']), {});
});

test('arrays: set by index, delete splices', () => {
  const doc: JsonObject = { hooks: { Stop: [{ hooks: [{ command: 'a' }, { command: 'b' }] }] } };
  let d = set(doc, ['hooks', 'Stop', 0, 'hooks', 1, 'command'], 'c');
  assert.equal(get(d, ['hooks', 'Stop', 0, 'hooks', 1, 'command']), 'c');
  d = del(d, ['hooks', 'Stop', 0, 'hooks', 0], doc);
  assert.deepEqual(get(d, ['hooks', 'Stop', 0, 'hooks']), [{ command: 'c' }]);
  d = set(d, ['hooks', 'Stop', 1], { hooks: [] });
  assert.equal((get(d, ['hooks', 'Stop']) as unknown[]).length, 2);
});

test('__proto__ is just a key', () => {
  const doc = JSON.parse('{"__proto__":{"x":1}}') as JsonObject;
  const d = set(doc, ['y'], 2);
  assert.ok(Object.prototype.hasOwnProperty.call(d, '__proto__'));
  assert.equal(Object.getPrototypeOf(d), Object.prototype);
  assert.equal(stringify(d), '{\n  "__proto__": {\n    "x": 1\n  },\n  "y": 2\n}\n');
});

test('renameKey keeps the place', () => {
  const doc: JsonObject = { m: { a: 1, b: 2, c: 3 } };
  assert.deepEqual(Object.keys(get(renameKey(doc, ['m'], 'b', 'z'), ['m']) as object), ['a', 'z', 'c']);
});

test('provider keys match the Go rule', () => {
  for (const k of ['ANTHROPIC_BASE_URL', 'ANTHROPIC_DEFAULT_OPUS_MODEL', 'CLAUDE_CODE_OAUTH_TOKEN', 'CLAUDE_CODE_USE_BEDROCK']) assert.ok(isProviderKey(k), k);
  for (const k of ['CLAUDE_CODE_MAX_OUTPUT_TOKENS', 'DISABLE_AUTOUPDATER', 'ENABLE_TOOL_SEARCH']) assert.ok(!isProviderKey(k), k);
});
