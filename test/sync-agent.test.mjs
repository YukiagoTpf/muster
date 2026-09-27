import { describe, test, before, after } from 'node:test'
import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { sanitizeMeta } from '../src/commands/sync-agent.mjs'
import { syncUnitHash } from '../src/sync.mjs'
import { loadConfig } from '../src/store.mjs'

const here = path.dirname(fileURLToPath(import.meta.url))
const bin = path.join(here, '..', 'bin', 'agentswap.mjs')

describe('__sync agent', () => {
  let tmp, appHome
  const env = () => ({
    ...process.env,
    AGENTSWAP_HOME: appHome,
  })

  function sync(args, { input } = {}) {
    const r = spawnSync(process.execPath, [bin, '__sync', ...args], {
      env: env(), encoding: 'utf8', input,
    })
    return { code: r.status, out: r.stdout, err: r.stderr }
  }
  function syncJson(args, opts) {
    const r = sync(args, opts)
    let parsed
    try { parsed = JSON.parse(r.out) } catch {
      throw new Error(`__sync ${args.join(' ')} exited ${r.code}: not JSON: ${r.err || r.out}`)
    }
    return { ...r, json: parsed }
  }

  before(() => {
    tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'agentswap-sync-agent-'))
    appHome = path.join(tmp, 'home')
    fs.mkdirSync(path.join(appHome, 'profiles'), { recursive: true, mode: 0o700 })
    process.env.AGENTSWAP_HOME = appHome
  })

  after(() => {
    fs.rmSync(tmp, { recursive: true, force: true })
  })

  test('sanitizeMeta fills defaults and filters invalid remotes/launchArgs', () => {
    assert.deepEqual(sanitizeMeta(undefined), { notes: '', launchArgs: null, unsetEnv: [], remotes: [] })
    assert.deepEqual(sanitizeMeta(null), { notes: '', launchArgs: null, unsetEnv: [], remotes: [] })
    assert.deepEqual(sanitizeMeta('nope'), { notes: '', launchArgs: null, unsetEnv: [], remotes: [] })
    assert.deepEqual(sanitizeMeta({
      notes: 'hi',
      launchArgs: ['--x', 1, '--y'],
      unsetEnv: ['A', 2, 'B'],
      remotes: ['ok', 'BAD NAME', 3, 'also_ok', '../x'],
    }), {
      notes: 'hi',
      launchArgs: ['--x', '--y'],
      unsetEnv: ['A', 'B'],
      remotes: ['also_ok', 'ok'], // sorted: set semantics
    })
    assert.deepEqual(sanitizeMeta({ launchArgs: '--flag' }).launchArgs, null)
    assert.equal(sanitizeMeta({ notes: 12 }).notes, '')
  })

  test('list hashes readable profiles and reports corrupt ones without failing', () => {
    const work = { model: 'm1', env: { TOKEN: 'x' } }
    fs.writeFileSync(path.join(appHome, 'profiles', 'work.json'), JSON.stringify(work))
    fs.writeFileSync(path.join(appHome, 'profiles', 'bad.json'), '{not json')
    const r = syncJson(['list'])
    assert.equal(r.code, 0)
    assert.equal(r.json.version, 2)
    assert.equal(r.json.profiles.work, syncUnitHash(work, {}))
    assert.equal(r.json.profiles.bad, undefined)
    assert.equal(r.json.errors.length, 1)
    assert.equal(r.json.errors[0].name, 'bad')
    assert.match(r.json.errors[0].detail, /not valid JSON/)
  })

  test('read round-trips profile + sanitized meta', () => {
    fs.writeFileSync(path.join(appHome, 'config.json'), JSON.stringify({
      version: 2,
      profiles: { work: { notes: 'from-config', launchArgs: ['--x'], unsetEnv: ['A'], remotes: ['vps'] } },
    }))
    const r = syncJson(['read', 'work'])
    assert.equal(r.code, 0)
    assert.equal(r.json.version, 2)
    assert.deepEqual(r.json.profile, { model: 'm1', env: { TOKEN: 'x' } })
    assert.deepEqual(r.json.meta, {
      notes: 'from-config', launchArgs: ['--x'], unsetEnv: ['A'], remotes: ['vps'],
    })
  })

  test('read missing name is usage; absent profile is not-found', () => {
    const usage = syncJson(['read'])
    assert.equal(usage.code, 1)
    assert.equal(usage.json.error, 'usage')
    const missing = syncJson(['read', 'nope'])
    assert.equal(missing.code, 1)
    assert.equal(missing.json.error, 'not-found')
    assert.match(missing.json.detail, /nope/)
  })

  test('write creates a profile from the stdin envelope', () => {
    const profile = { model: 'created' }
    const r = syncJson(['write', 'fresh'], {
      input: JSON.stringify({ profile, meta: { notes: 'n', remotes: ['office'] } }),
    })
    assert.equal(r.code, 0, r.out + r.err)
    assert.equal(r.json.hash, syncUnitHash(profile, { notes: 'n', remotes: ['office'] }))
    assert.deepEqual(
      JSON.parse(fs.readFileSync(path.join(appHome, 'profiles', 'fresh.json'), 'utf8')),
      profile,
    )
  })

  test('write with expectedHash=null refuses when the profile exists', () => {
    const r = syncJson(['write', 'fresh'], {
      input: JSON.stringify({ profile: { model: 'other' }, expectedHash: null }),
    })
    assert.equal(r.code, 1)
    assert.equal(r.json.error, 'conflict')
    assert.deepEqual(
      JSON.parse(fs.readFileSync(path.join(appHome, 'profiles', 'fresh.json'), 'utf8')),
      { model: 'created' },
    )
  })

  test('write with a stale expectedHash refuses (conflict)', () => {
    const r = syncJson(['write', 'fresh'], {
      input: JSON.stringify({ profile: { model: 'stale' }, expectedHash: 'deadbeef' }),
    })
    assert.equal(r.code, 1)
    assert.equal(r.json.error, 'conflict')
  })

  test('write without expectedHash overwrites (force)', () => {
    const profile = { model: 'forced' }
    const r = syncJson(['write', 'fresh'], { input: JSON.stringify({ profile }) })
    assert.equal(r.code, 0)
    assert.equal(r.json.hash, syncUnitHash(profile, {}))
    assert.deepEqual(
      JSON.parse(fs.readFileSync(path.join(appHome, 'profiles', 'fresh.json'), 'utf8')),
      profile,
    )
  })

  test('write persists meta including remotes membership', () => {
    const profile = { model: 'with-meta' }
    const r = syncJson(['write', 'fresh'], {
      input: JSON.stringify({
        profile,
        meta: { notes: 'kept', launchArgs: ['--y'], unsetEnv: ['Z'], remotes: ['vps', 'devbox'] },
      }),
    })
    assert.equal(r.code, 0)
    const cfg = loadConfig()
    assert.deepEqual(cfg.profiles.fresh, {
      notes: 'kept', launchArgs: ['--y'], unsetEnv: ['Z'], remotes: ['devbox', 'vps'],
    })
  })

  test('write with matching expectedHash succeeds (CAS create-then-update)', () => {
    const before = JSON.parse(fs.readFileSync(path.join(appHome, 'profiles', 'fresh.json'), 'utf8'))
    const beforeMeta = loadConfig().profiles.fresh
    const next = { model: 'cas-ok' }
    const r = syncJson(['write', 'fresh'], {
      input: JSON.stringify({ profile: next, expectedHash: syncUnitHash(before, beforeMeta) }),
    })
    assert.equal(r.code, 0)
    assert.equal(r.json.hash, syncUnitHash(next, {}))
  })

  test('unknown sub is an error envelope', () => {
    const r = syncJson(['nope'])
    assert.equal(r.code, 1)
    assert.equal(r.json.error, 'unknown-sub')
    assert.match(r.json.detail, /nope/)
  })
})
