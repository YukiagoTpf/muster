import { describe, test, before, after } from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { loadConfig, gcRunFilesByOwner, KEPT_RUN_SUFFIX, writeAtomic, configDirFingerprint, agentSettingsPath } from '../src/store.mjs'

// Creating symlinks needs a privilege on Windows (Developer Mode); probe it.
const symlinkSkip = (() => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agentswap-symlink-probe-'))
  try { fs.symlinkSync('x', path.join(dir, 'l')); return false }
  catch (e) { return `cannot create symlinks here (${e.code})` }
  finally { fs.rmSync(dir, { recursive: true, force: true }) }
})()

describe('store config', () => {
  let tmp, appHome

  before(() => {
    tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'agentswap-store-'))
    appHome = path.join(tmp, 'agentswap-home')
    process.env.AGENTSWAP_HOME = appHome
  })

  after(() => {
    fs.rmSync(tmp, { recursive: true, force: true })
  })

  test('loadConfig returns fresh nested defaults (no ghost metadata, no persist)', () => {
    const first = loadConfig()
    assert.equal(fs.existsSync(path.join(appHome, 'config.json')), false, 'must not persist the default')
    first.profiles.ghost = { notes: 'never saved' }
    first.defaultLaunchArgs.push('--ghost')
    const second = loadConfig()
    assert.deepEqual(second.profiles, {}, 'a mutated default must not leak into later loads')
    assert.deepEqual(second.defaultLaunchArgs, ['--dangerously-skip-permissions'])
  })

  test('run/ GC collects a dead owner\'s session file but never one kept for inspection', () => {
    const runDir = path.join(appHome, 'run')
    fs.mkdirSync(runDir, { recursive: true })
    const dead = 2 ** 22 + 12345 // no live process has a pid this large
    const stale = path.join(runDir, `work-${dead}-abcdef012345.json`)
    const kept = path.join(runDir, `work-${dead}-abcdef012345${KEPT_RUN_SUFFIX}`)
    fs.writeFileSync(stale, '{}')
    fs.writeFileSync(kept, '{"model":"only-copy"}')
    gcRunFilesByOwner()
    assert.equal(fs.existsSync(stale), false, 'a dead session\'s temp file is garbage')
    assert.equal(fs.existsSync(kept), true, 'a kept file may be the only copy of the session')
  })

  test('a relative CLAUDE_CONFIG_DIR binds state to the directory it resolves to', () => {
    const prevDir = process.env.CLAUDE_CONFIG_DIR
    const prevCwd = process.cwd()
    const repoA = path.join(tmp, 'repo-a')
    const repoB = path.join(tmp, 'repo-b')
    fs.mkdirSync(repoA, { recursive: true })
    fs.mkdirSync(repoB, { recursive: true })
    process.env.CLAUDE_CONFIG_DIR = '.claude-dev'
    try {
      process.chdir(repoA)
      const a = { fp: configDirFingerprint(), settings: agentSettingsPath() }
      process.chdir(repoB)
      const b = { fp: configDirFingerprint(), settings: agentSettingsPath() }
      assert.ok(path.isAbsolute(a.settings), a.settings)
      assert.notEqual(a.settings, b.settings)
      assert.notEqual(a.fp, b.fp, 'two different live files must not share one state/journal')
    } finally {
      process.chdir(prevCwd)
      if (prevDir === undefined) delete process.env.CLAUDE_CONFIG_DIR
      else process.env.CLAUDE_CONFIG_DIR = prevDir
    }
  })

  test('writeAtomic writes through a symlink instead of replacing it', { skip: symlinkSkip }, () => {
    // e.g. ~/.claude/settings.json linked into a dotfiles repo
    const dir = path.join(tmp, 'links')
    fs.mkdirSync(path.join(dir, 'dotfiles'), { recursive: true })
    const target = path.join(dir, 'dotfiles', 'settings.json')
    const link = path.join(dir, 'settings.json')
    fs.writeFileSync(target, '{"model":"old"}')
    fs.symlinkSync(path.join('dotfiles', 'settings.json'), link)
    writeAtomic(link, '{"model":"new"}')
    assert.ok(fs.lstatSync(link).isSymbolicLink(), 'the link must survive the write')
    assert.equal(fs.readFileSync(target, 'utf8'), '{"model":"new"}')

    // a dangling link: the write creates the file it points at
    const dangling = path.join(dir, 'dangling.json')
    fs.symlinkSync(path.join('dotfiles', 'missing.json'), dangling)
    writeAtomic(dangling, '{}')
    assert.ok(fs.lstatSync(dangling).isSymbolicLink())
    assert.equal(fs.readFileSync(path.join(dir, 'dotfiles', 'missing.json'), 'utf8'), '{}')
  })
})
