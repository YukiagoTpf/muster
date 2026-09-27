import { describe, test, before, after } from 'node:test'
import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { installFakeSsh } from './helpers/fake-ssh.mjs'
import { writeSyncState, readSyncState } from '../src/sync.mjs'
import { validateRemote, DEFAULT_BIN } from '../src/remotes.mjs'

const here = path.dirname(fileURLToPath(import.meta.url))
const bin = path.join(here, '..', 'bin', 'agentswap.mjs')
const skipSsh = process.platform === 'win32'

describe('remote registry', () => {
  let tmp, appHome, sshDir, origPath
  const env = () => ({
    ...process.env,
    AGENTSWAP_HOME: appHome,
    PATH: `${sshDir}${path.delimiter}${process.env.PATH}`,
  })

  function agentswap(...args) {
    const r = spawnSync(process.execPath, [bin, ...args], { env: env(), encoding: 'utf8' })
    return { code: r.status, out: r.stdout, err: r.stderr }
  }
  function agentswapJson(...args) {
    const r = agentswap(...args)
    let parsed
    try { parsed = JSON.parse(r.out) } catch {
      throw new Error(`agentswap ${args.join(' ')} exited ${r.code}: ${r.err || r.out}`)
    }
    return { ...r, json: parsed }
  }

  before(() => {
    tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'agentswap-remotes-'))
    appHome = path.join(tmp, 'local')
    const remoteHome = path.join(tmp, 'remote-home')
    fs.mkdirSync(remoteHome, { recursive: true })
    sshDir = path.join(tmp, 'bin')
    installFakeSsh(sshDir, { 'office-mac': remoteHome, testhost: remoteHome })
    origPath = process.env.PATH
    process.env.AGENTSWAP_HOME = appHome
    process.env.PATH = `${sshDir}${path.delimiter}${origPath}`
  })

  after(() => {
    process.env.PATH = origPath
    fs.rmSync(tmp, { recursive: true, force: true })
  })

  test('validateRemote rejects bad names, whitespace hosts, empty bins', () => {
    assert.equal(validateRemote('ok', { host: 'h' }), null)
    assert.match(validateRemote('-bad', { host: 'h' }), /invalid remote name/)
    assert.match(validateRemote('../x', { host: 'h' }), /invalid remote name/)
    assert.match(validateRemote('ok', { host: '' }), /invalid host/)
    assert.match(validateRemote('ok', { host: 'has space' }), /invalid host/)
    assert.match(validateRemote('ok', { host: 'h', bin: '' }), /invalid bin/)
    assert.equal(DEFAULT_BIN, 'agentswap')
  })

  test('add missing name/host is a usage error', () => {
    const r = agentswap('remote', 'add', '--json')
    assert.equal(r.code, 1)
    assert.match(r.out, /usage: agentswap remote add/)
  })

  test('add validation failures (name, host)', () => {
    const badName = agentswapJson('remote', 'add', '-bad', '--host', 'testhost', '--json')
    assert.equal(badName.code, 1)
    assert.equal(badName.json.error, 'invalid-remote')

    const badHost = agentswapJson('remote', 'add', 'office', '--host', 'has space', '--json')
    assert.equal(badHost.code, 1)
    assert.equal(badHost.json.error, 'invalid-remote')
  })

  test('add probes via fake ssh using the repo bin, then records the remote', { skip: skipSsh }, () => {
    const r = agentswapJson('remote', 'add', 'office', '--host', 'office-mac', '--bin', bin, '--json')
    assert.equal(r.code, 0, r.err || r.out)
    assert.equal(r.json.version, 2)
    assert.equal(r.json.added, 'office')
    assert.deepEqual(r.json.remote, { host: 'office-mac', bin })
    const cfg = JSON.parse(fs.readFileSync(path.join(appHome, 'config.json'), 'utf8'))
    assert.deepEqual(cfg.remotes.office, { host: 'office-mac', bin })
  })

  test('duplicate add is rejected', { skip: skipSsh }, () => {
    const r = agentswapJson('remote', 'add', 'office', '--host', 'office-mac', '--bin', bin, '--json')
    assert.equal(r.code, 1)
    assert.equal(r.json.error, 'exists')
  })

  test('ls table + json', { skip: skipSsh }, () => {
    const table = agentswap('remote', 'ls')
    assert.equal(table.code, 0)
    assert.match(table.out, /NAME/)
    assert.match(table.out, /HOST/)
    assert.match(table.out, /BIN/)
    assert.match(table.out, /office/)
    assert.match(table.out, /office-mac/)

    const j = agentswapJson('remote', 'ls', '--json')
    assert.equal(j.code, 0)
    assert.equal(j.json.version, 2)
    assert.equal(j.json.remotes.length, 1)
    assert.equal(j.json.remotes[0].name, 'office')
    assert.equal(j.json.remotes[0].host, 'office-mac')
  })

  test('rm removes the registry entry, strips membership, and clears the sync.json subtree', { skip: skipSsh }, () => {
    const cfgPath = path.join(appHome, 'config.json')
    const cfg = JSON.parse(fs.readFileSync(cfgPath, 'utf8'))
    cfg.profiles = cfg.profiles || {}
    cfg.profiles.work = { notes: '', launchArgs: null, unsetEnv: [], remotes: ['office', 'devbox'] }
    fs.writeFileSync(cfgPath, JSON.stringify(cfg, null, 2) + '\n')

    writeSyncState({
      version: 1,
      remotes: {
        office: { profiles: { work: { hash: 'abc', ts: '2026-01-01T00:00:00.000Z' } } },
        other: { profiles: { work: { hash: 'def', ts: '2026-01-01T00:00:00.000Z' } } },
      },
    })

    const r = agentswapJson('remote', 'rm', 'office', '--json')
    assert.equal(r.code, 0, r.err || r.out)
    assert.equal(r.json.removed, 'office')

    const after = JSON.parse(fs.readFileSync(cfgPath, 'utf8'))
    assert.equal(after.remotes.office, undefined)
    assert.deepEqual(after.profiles.work.remotes, ['devbox'])

    const state = readSyncState()
    assert.equal(state.remotes.office, undefined)
    assert.ok(state.remotes.other)
  })

  test('rm of an unknown remote fails', () => {
    const r = agentswapJson('remote', 'rm', 'nope', '--json')
    assert.equal(r.code, 1)
    assert.equal(r.json.error, 'not-found')
  })

  test('unknown sub is a usage error listing the three subs', () => {
    const r = agentswap('remote', 'frob')
    assert.equal(r.code, 1)
    assert.match(r.out, /usage: agentswap remote add/)
    assert.match(r.out, /\bls\b/)
    assert.match(r.out, /\brm\b/)
  })

  test('ls on an empty registry prints the add hint', () => {
    // Isolated home so it does not depend on earlier tests having removed office.
    const empty = path.join(tmp, 'empty-home')
    fs.mkdirSync(empty, { recursive: true })
    const r = spawnSync(process.execPath, [bin, 'remote', 'ls'], {
      env: { ...env(), AGENTSWAP_HOME: empty }, encoding: 'utf8',
    })
    assert.equal(r.status, 0)
    assert.match(r.stdout, /no remotes — add one with: agentswap remote add/)
  })
})
