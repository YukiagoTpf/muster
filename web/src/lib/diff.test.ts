import assert from 'node:assert/strict';
import { test } from 'node:test';
import { fold, sideBySide } from './diff.ts';

test('pairs changed lines and keeps the rest aligned', () => {
  const rows = sideBySide('a\nb\nc\nd\n', 'a\nB\nc\nd\ne\n');
  assert.deepEqual(
    rows.map((r) => r.kind),
    ['same', 'change', 'same', 'same', 'add'],
  );
  assert.equal(rows[1].left?.text, 'b');
  assert.equal(rows[1].right?.text, 'B');
  assert.equal(rows[4].right?.n, 5);
});

test('a missing side is all additions or deletions', () => {
  assert.deepEqual(
    sideBySide('', 'x\ny').map((r) => r.kind),
    ['add', 'add'],
  );
  assert.deepEqual(
    sideBySide('x\n', '').map((r) => r.kind),
    ['del'],
  );
});

test('fold keeps context around changes', () => {
  const a = Array.from({ length: 20 }, (_, i) => 'l' + i).join('\n');
  const b = a.replace('l10', 'X');
  const folded = fold(sideBySide(a, b), 2);
  assert.equal(folded[0], null);
  assert.equal(folded.filter((r) => r !== null).length, 5);
  assert.equal(folded[folded.length - 1], null);
});
