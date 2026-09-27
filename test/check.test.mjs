import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import path from 'node:path';
import { readFileSync } from 'node:fs';
import {
  ROOT,
  patternToRegex,
  parseFrontmatter,
  validateSkill,
  extractRelativeLinks,
  parseRoutingTable,
  routeMatches,
  validateCommitMessage,
} from '../scripts/check.mjs';

test('patternToRegex: ** matches across segments', () => {
  const re = patternToRegex('src/commands/**');
  assert.ok(re.test('src/commands/use.mjs'));
  assert.ok(re.test('src/commands/sub/dir/x.mjs'));
  assert.ok(!re.test('src/use.mjs'));
});

test('patternToRegex: * stays within a segment', () => {
  const re = patternToRegex('*.md');
  assert.ok(re.test('README.md'));
  assert.ok(!re.test('docs/architecture.md'));
});

test('patternToRegex: exact path matches itself only', () => {
  const re = patternToRegex('package.json');
  assert.ok(re.test('package.json'));
  assert.ok(!re.test('x/package.json'));
});

test('parseFrontmatter: parses name and description', () => {
  const fm = parseFrontmatter('---\nname: agentswap\ndescription: hello world\n---\nbody');
  assert.equal(fm.data.name, 'agentswap');
  assert.equal(fm.data.description, 'hello world');
  assert.equal(fm.body, 'body');
});

test('parseFrontmatter: throws without fences', () => {
  assert.throws(() => parseFrontmatter('name: agentswap\n'), /opening frontmatter/);
  assert.throws(() => parseFrontmatter('---\nname: agentswap\n'), /closing frontmatter/);
});

test('validateSkill: accepts the bundled skill', () => {
  const text = readFileSync(path.join(ROOT, 'skills', 'agentswap', 'SKILL.md'), 'utf8');
  assert.deepEqual(validateSkill(path.join(ROOT, 'skills', 'agentswap'), text), []);
});

test('validateSkill: rejects name mismatch and bad shape', () => {
  const text = '---\nname: other\ndescription: x\n---\n';
  const failures = validateSkill(path.join(ROOT, 'skills', 'agentswap'), text);
  assert.ok(failures.some((f) => f.includes('does not match directory')));
});

test('validateSkill: rejects overlong description', () => {
  const text = `---\nname: agentswap\ndescription: ${'x'.repeat(1025)}\n---\n`;
  const failures = validateSkill(path.join(ROOT, 'skills', 'agentswap'), text);
  assert.ok(failures.some((f) => f.includes('1024')));
});

test('extractRelativeLinks: keeps local, skips URLs and anchors', () => {
  const md = '[a](docs/x.md) [b](https://example.com) [c](#section) [d](./y.md)';
  assert.deepEqual(extractRelativeLinks(md), ['docs/x.md', './y.md']);
});

test('parseRoutingTable: parses routes and skips separator', () => {
  const md = [
    '# Index',
    '',
    '| Changed path | Docs to check |',
    '| --- | --- |',
    '| `src/**` | `docs/a.md`, `docs/b.md` |',
    '| `.gitignore` | — |',
    '',
  ].join('\n');
  const routes = parseRoutingTable(md);
  assert.equal(routes.length, 2);
  assert.deepEqual(routes[0].patterns, ['src/**']);
  assert.deepEqual(routes[0].docs, ['docs/a.md', 'docs/b.md']);
  assert.deepEqual(routes[1].docs, []);
});

test('parseRoutingTable: data rows mentioning path/doc are not eaten as headers', () => {
  const md = [
    '| Changed path | Docs to check |',
    '| --- | --- |',
    '| `src/paths.mjs` | `docs/architecture.md` |',
    '',
  ].join('\n');
  const routes = parseRoutingTable(md);
  assert.equal(routes.length, 1);
  assert.deepEqual(routes[0].patterns, ['src/paths.mjs']);
});

test('routeMatches: exact and glob routes', () => {
  const routes = parseRoutingTable(
    '| Changed path | Docs to check |\n| --- | --- |\n| `package.json` | `README.md` |\n| `src/**` | `docs/a.md` |\n',
  );
  assert.ok(routeMatches(routes, 'package.json'));
  assert.ok(routeMatches(routes, 'src/store.mjs'));
  assert.ok(!routeMatches(routes, 'test/x.test.mjs'));
});

test('validateCommitMessage: accepts conventional subjects', () => {
  assert.deepEqual(validateCommitMessage('docs: add routing table\n\nbody'), []);
  assert.deepEqual(validateCommitMessage('fix(lock): close the race'), []);
});

test('validateCommitMessage: rejects bad subjects', () => {
  assert.ok(validateCommitMessage('updated stuff').length > 0);
  assert.ok(validateCommitMessage('wip: whatever').length > 0);
  assert.ok(validateCommitMessage(`fix: ${'x'.repeat(80)}`).some((f) => f.includes('72')));
});

test('check script passes on the repo itself', () => {
  const out = execFileSync(process.execPath, [path.join(ROOT, 'scripts', 'check.mjs')], { encoding: 'utf8' });
  assert.match(out, /checks passed/);
});
