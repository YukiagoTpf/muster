#!/usr/bin/env node
// Repo guardrails for agentswap development. Zero dependencies, Node >= 20.
//
// Usage:
//   node scripts/check.mjs                    run all static checks
//   node scripts/check.mjs --commit-msg <file>  validate a commit message
//
// Checks: zero runtime deps, skill frontmatter contract, doc links exist,
// doc-ownership routing covers every tracked path, local redline grep.

import { execFileSync } from 'node:child_process';
import { existsSync, readFileSync, readdirSync } from 'node:fs';
import path from 'node:path';
import process from 'node:process';
import { fileURLToPath } from 'node:url';

export const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');

const COMMIT_TYPES = ['build', 'chore', 'ci', 'docs', 'feat', 'fix', 'perf', 'refactor', 'revert', 'style', 'test'];
const TEXT_EXTENSIONS = new Set(['.md', '.mjs', '.js', '.json', '.html', '.txt', '.yaml', '.yml']);
const BINARY_EXTENSIONS = new Set(['.png', '.jpg', '.jpeg', '.gif', '.pdf', '.zip', '.ico']);

// ---------- pure helpers (unit-tested) ----------

export function patternToRegex(pattern) {
  const body = pattern
    .replace(/[.+?^${}()|[\]\\]/g, '\\$&')
    .replace(/\*\*|\*/g, (m) => (m === '**' ? '.*' : '[^/]*'));
  return new RegExp(`^${body}$`);
}

export function parseFrontmatter(text) {
  if (!text.startsWith('---\n')) throw new Error('missing opening frontmatter fence');
  let end = text.indexOf('\n---\n', 4);
  if (end === -1 && text.endsWith('\n---')) end = text.length - 4;
  if (end === -1) throw new Error('missing closing frontmatter fence');
  const data = {};
  for (const line of text.slice(4, end).split('\n')) {
    const m = line.match(/^([a-zA-Z][a-zA-Z0-9_-]*):\s*(.*)$/);
    if (m) data[m[1]] = m[2];
  }
  return { data, body: text.slice(end + 5) };
}

export function validateSkill(skillDir, text) {
  const failures = [];
  let fm;
  try {
    fm = parseFrontmatter(text);
  } catch (e) {
    return [`frontmatter: ${e.message}`];
  }
  const dirName = path.basename(skillDir);
  const name = fm.data.name;
  if (!name) {
    failures.push('frontmatter: missing name');
  } else {
    if (name !== dirName) failures.push(`frontmatter: name "${name}" does not match directory "${dirName}"`);
    if (!/^[a-z0-9]+(-[a-z0-9]+)*$/.test(name)) failures.push(`frontmatter: name "${name}" is not kebab-case`);
    if (name.length > 64) failures.push('frontmatter: name longer than 64 chars');
  }
  const desc = fm.data.description;
  if (!desc || !desc.trim()) {
    failures.push('frontmatter: missing description');
  } else if (desc.length > 1024) {
    failures.push('frontmatter: description longer than 1024 chars');
  }
  return failures;
}

export function extractRelativeLinks(markdown) {
  const links = [];
  const re = /\[[^\]]*\]\(([^)]+)\)/g;
  let m;
  while ((m = re.exec(markdown))) {
    const target = m[1].trim().split('#')[0];
    if (!target || /^(https?:|mailto:)/.test(target)) continue;
    links.push(target);
  }
  return links;
}

function backticked(cell) {
  return [...cell.matchAll(/`([^`]+)`/g)].map((m) => m[1]).filter((t) => t !== '—' && t !== '');
}

export function parseRoutingTable(markdown) {
  const routes = [];
  let table = [];
  const flush = () => {
    if (table.length >= 3) {
      const header = table[0].split('|').slice(1, -1).map((c) => c.trim());
      if (header.length >= 2 && /path/i.test(header[0]) && /doc/i.test(header[1])) {
        for (const row of table.slice(2)) {
          const cells = row.split('|').slice(1, -1).map((c) => c.trim());
          if (cells.length < 2) continue;
          const patterns = backticked(cells[0]);
          if (patterns.length > 0) routes.push({ patterns, docs: backticked(cells[1]) });
        }
      }
    }
    table = [];
  };
  for (const line of markdown.split('\n')) {
    if (line.trim().startsWith('|')) table.push(line);
    else flush();
  }
  flush();
  return routes;
}

export function routeMatches(routes, file) {
  return routes.some((r) => r.patterns.some((p) => patternToRegex(p).test(file)));
}

export function validateCommitMessage(subject) {
  const failures = [];
  const line = subject.split('\n')[0].trim();
  if (!line) {
    failures.push('empty subject');
    return failures;
  }
  if (!new RegExp(`^(${COMMIT_TYPES.join('|')})(\\(.+\\))?: .+`).test(line)) {
    failures.push(`subject "${line}" is not a conventional commit (type: subject; types: ${COMMIT_TYPES.join(', ')})`);
  }
  if (line.length > 72) failures.push(`subject is ${line.length} chars, keep it under 72`);
  return failures;
}

// ---------- repo checks ----------

function gitLsFiles() {
  return execFileSync('git', ['ls-files'], { cwd: ROOT, encoding: 'utf8' }).trim().split('\n').filter(Boolean);
}

function loadRedlines() {
  const file = path.join(ROOT, '.redlines.local.json');
  if (!existsSync(file)) return null;
  const parsed = JSON.parse(readFileSync(file, 'utf8'));
  if (!Array.isArray(parsed.terms)) throw new Error('.redlines.local.json: "terms" must be an array');
  return parsed.terms.filter((t) => typeof t === 'string' && t.trim());
}

function redlineHitsInText(text, where) {
  const terms = loadRedlines();
  if (!terms || terms.length === 0) return [];
  const failures = [];
  const lower = text.toLowerCase();
  for (const t of terms) {
    if (lower.includes(t.toLowerCase())) failures.push(`${where}: redline term "${t}"`);
  }
  return failures;
}

function checkZeroDeps() {
  const pkg = JSON.parse(readFileSync(path.join(ROOT, 'package.json'), 'utf8'));
  const deps = pkg.dependencies;
  if (deps && Object.keys(deps).length > 0) {
    return [`package.json declares runtime dependencies: ${Object.keys(deps).join(', ')}`];
  }
  return [];
}

function checkSkills() {
  const failures = [];
  const skillsDir = path.join(ROOT, 'skills');
  if (!existsSync(skillsDir)) return failures;
  for (const entry of readdirSync(skillsDir)) {
    const skillFile = path.join(skillsDir, entry, 'SKILL.md');
    if (!existsSync(skillFile)) {
      failures.push(`skills/${entry}: no SKILL.md`);
      continue;
    }
    const text = readFileSync(skillFile, 'utf8');
    for (const f of validateSkill(path.join(skillsDir, entry), text)) failures.push(`skills/${entry}/SKILL.md: ${f}`);
    for (const link of extractRelativeLinks(text)) {
      if (!existsSync(path.join(skillsDir, entry, link))) failures.push(`skills/${entry}/SKILL.md: broken link ${link}`);
    }
  }
  return failures;
}

function checkDocLinks(tracked) {
  const failures = [];
  for (const file of tracked.filter((f) => f.endsWith('.md'))) {
    const text = readFileSync(path.join(ROOT, file), 'utf8');
    for (const link of extractRelativeLinks(text)) {
      if (!existsSync(path.join(ROOT, path.dirname(file), link))) failures.push(`${file}: broken link ${link}`);
    }
  }
  return failures;
}

function checkRouting(tracked) {
  const failures = [];
  const indexFile = path.join(ROOT, 'docs', 'index.md');
  if (!existsSync(indexFile)) return ['docs/index.md missing (doc routing table)'];
  const routes = parseRoutingTable(readFileSync(indexFile, 'utf8'));
  if (routes.length === 0) failures.push('docs/index.md: no routing table found');
  for (const file of tracked) {
    if (!routeMatches(routes, file)) failures.push(`no doc route for tracked path: ${file}`);
  }
  for (const route of routes) {
    for (const doc of route.docs) {
      if (!existsSync(path.join(ROOT, doc))) failures.push(`docs/index.md: owner doc does not exist: ${doc}`);
    }
  }
  return failures;
}

function isTextFile(file) {
  const ext = path.extname(file).toLowerCase();
  if (BINARY_EXTENSIONS.has(ext)) return false;
  if (ext === '') return true;
  return TEXT_EXTENSIONS.has(ext);
}

function checkRedlines(tracked) {
  const terms = loadRedlines();
  if (terms === null) {
    console.log('  redlines: .redlines.local.json not found, skipping');
    return [];
  }
  if (terms.length === 0) return [];
  const failures = [];
  for (const file of tracked) {
    if (!isTextFile(file)) continue;
    const lines = readFileSync(path.join(ROOT, file), 'utf8').split('\n');
    lines.forEach((line, i) => {
      const lower = line.toLowerCase();
      for (const t of terms) {
        if (lower.includes(t.toLowerCase())) {
          failures.push(`${file}:${i + 1}: redline term "${t}"`);
          break;
        }
      }
    });
  }
  return failures;
}

// ---------- CLI ----------

function report(name, failures) {
  if (failures.length === 0) {
    console.log(`  ${name}: ok`);
    return false;
  }
  console.log(`  ${name}: FAIL (${failures.length})`);
  for (const f of failures) console.log(`    - ${f}`);
  return true;
}

function main() {
  const args = process.argv.slice(2);
  if (args[0] === '--commit-msg') {
    const file = args[1];
    if (!file) {
      console.error('usage: node scripts/check.mjs --commit-msg <file>');
      process.exit(2);
    }
    const text = readFileSync(path.resolve(file), 'utf8');
    const failures = [...validateCommitMessage(text), ...redlineHitsInText(text, 'commit message')];
    if (failures.length > 0) {
      console.error('Commit message check failed:');
      for (const f of failures) console.error(`- ${f}`);
      process.exit(1);
    }
    console.log('commit message: ok');
    return;
  }

  const tracked = gitLsFiles();
  const checks = [
    ['zero dependencies', checkZeroDeps],
    ['skill contracts', checkSkills],
    ['doc links', () => checkDocLinks(tracked)],
    ['doc routing', () => checkRouting(tracked)],
    ['redlines', () => checkRedlines(tracked)],
  ];
  let failed = false;
  for (const [name, fn] of checks) {
    let found;
    try {
      found = fn();
    } catch (e) {
      found = [`${name}: ${e.message}`];
    }
    if (report(name, found)) failed = true;
  }
  if (failed) process.exit(1);
  console.log(`checks passed (${checks.length} checks, ${tracked.length} tracked files)`);
}

const isMain = process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url);
if (isMain) main();
