import { test } from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { installFakeSsh } from './helpers/fake-ssh.mjs'

// End-to-end sync tests over a FAKE ssh that forwards to second agentswap
// homes — no real server needed. POSIX-only (the shim is an extensionless
// executable script); Windows transport behavior is covered by unit tests.

const here = path.dirname(fileURLToPath(import.meta.url))
const bin = path.join(here, '..', 'bin', 'agentswap.mjs')
const skip = process.platform === 'win32'

function setup(remotes = ['vps']) {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'agentswap-sync-e2e-'))
  const local = path.join(tmp, 'local')
  const claude = path.join(tmp, 'claude')
  fs.mkdirSync(claude, { recursive: true })
  const homes = {}
  for (const r of remotes) homes[r] = path.join(tmp, r)
  const sshDir = path.join(tmp, 'bin')
  fs.mkdirSync(sshDir, { recursive: true })
  installFakeSsh(sshDir, homes)

  const env = {
    ...process.env,
    AGENTSWAP_HOME: local,
    CLAUDE_CONFIG_DIR: claude,
    EDITOR: 'true',
    VISUAL: 'true',
    PATH: `${sshDir}${path.delimiter}${process.env.PATH}`,
  }
  const agentswap = (...args) => {
    const r = spawnSync(process.execPath, [bin, ...args], { env, encoding: 'utf8' })
    return { code: r.status, out: r.stdout, err: r.stderr }
  }
  const json = (...args) => {
    const r = agentswap(...args)
    if (r.code !== 0) throw new Error(`agentswap ${args.join(' ')} exited ${r.code}: ${r.err || r.out}`)
    return JSON.parse(r.out)
  }
  for (const r of remotes) {
    const res = agentswap('remote', 'add', r, '--host', r, '--bin', bin)
    if (res.code !== 0) throw new Error(`remote add ${r}: ${res.err || res.out}`)
  }

  const profilePath = (home, name) => path.join(home, 'profiles', `${name}.json` )
  return {
    tmp, local, agentswap, json,
    writeLocalProfile: (name, obj) => {
      fs.mkdirSync(path.join(local, 'profiles'), { recursive: true })
      fs.writeFileSync(profilePath(local, name), JSON.stringify(obj, null, 2))
    },
    readLocalProfile: (name) => JSON.parse(fs.readFileSync(profilePath(local, name), 'utf8')),
    writeRemoteProfile: (remote, name, obj) => {
      fs.mkdirSync(path.join(homes[remote], 'profiles'), { recursive: true })
      fs.writeFileSync(profilePath(homes[remote], name), JSON.stringify(obj, null, 2))
    },
    readRemoteProfile: (remote, name) => JSON.parse(fs.readFileSync(profilePath(homes[remote], name), 'utf8')),
    readLocalConfig: () => JSON.parse(fs.readFileSync(path.join(local, 'config.json'), 'utf8')),
    cleanup: () => fs.rmSync(tmp, { recursive: true, force: true }),
  }
}

test('push creates the remote profile, records membership and baseline', { skip }, () => {
  const t = setup()
  try {
    assert.equal(t.agentswap('add', 'kimi').code, 0)
    const r = t.json('push', 'kimi', 'vps', '--json')
    assert.equal(r.results[0].status, 'pushed')
    assert.ok(t.readRemoteProfile('vps', 'kimi'))
    assert.deepEqual(t.readLocalConfig().profiles.kimi.remotes, ['vps'])
    const syncState = JSON.parse(fs.readFileSync(path.join(t.local, 'sync.json'), 'utf8'))
    assert.ok(syncState.remotes.vps.profiles.kimi.hash)
  } finally { t.cleanup() }
})

test('pushing twice is in-sync', { skip }, () => {
  const t = setup()
  try {
    t.agentswap('add', 'kimi')
    t.json('push', 'kimi', 'vps', '--json')
    const r = t.json('push', 'kimi', 'vps', '--json')
    assert.equal(r.results[0].status, 'in-sync')
  } finally { t.cleanup() }
})

test('sync pulls when only the remote moved', { skip }, () => {
  const t = setup()
  try {
    t.agentswap('add', 'kimi')
    t.json('push', 'kimi', 'vps', '--json')
    t.writeRemoteProfile('vps', 'kimi', { model: 'remote-take' })
    const r = t.json('sync', '--json')
    assert.equal(r.results[0].status, 'pulled')
    assert.equal(t.readLocalProfile('kimi').model, 'remote-take')
  } finally { t.cleanup() }
})

test('sync pushes when only local moved', { skip }, () => {
  const t = setup()
  try {
    t.agentswap('add', 'kimi')
    t.json('push', 'kimi', 'vps', '--json')
    t.writeLocalProfile('kimi', { model: 'local-take' })
    const r = t.json('sync', '--json')
    assert.equal(r.results[0].status, 'pushed')
    assert.equal(t.readRemoteProfile('vps', 'kimi').model, 'local-take')
  } finally { t.cleanup() }
})

test('sync refuses when both sides moved; --force-push resolves', { skip }, () => {
  const t = setup()
  try {
    t.agentswap('add', 'kimi')
    t.json('push', 'kimi', 'vps', '--json')
    t.writeLocalProfile('kimi', { model: 'local-take' })
    t.writeRemoteProfile('vps', 'kimi', { model: 'remote-take' })
    const conflict = t.agentswap('sync', '--json')
    assert.equal(conflict.code, 1)
    assert.match(conflict.out, /conflict/)
    const forced = t.json('sync', '--force-push', '--json')
    assert.equal(forced.results[0].status, 'pushed')
    assert.equal(t.readRemoteProfile('vps', 'kimi').model, 'local-take')
  } finally { t.cleanup() }
})

test('sync discovers and pulls a profile created on the remote', { skip }, () => {
  const t = setup()
  try {
    t.writeRemoteProfile('vps', 'newone', { model: 'born-on-remote' })
    const r = t.json('sync', '--json')
    assert.equal(r.results[0].status, 'pulled')
    assert.equal(t.readLocalProfile('newone').model, 'born-on-remote')
  } finally { t.cleanup() }
})

test('local delete does not propagate: sync re-pulls the remote copy', { skip }, () => {
  const t = setup()
  try {
    t.agentswap('add', 'kimi')
    t.json('push', 'kimi', 'vps', '--json')
    assert.equal(t.agentswap('rm', 'kimi', '--force').code, 0)
    const r = t.json('sync', '--json')
    assert.equal(r.results[0].status, 'pulled')
    assert.ok(t.readLocalProfile('kimi'))
  } finally { t.cleanup() }
})

test('push to and sync across two remotes; disagreement blocks propagation', { skip }, () => {
  const t = setup(['vps', 'devbox'])
  try {
    t.agentswap('add', 'kimi')
    const r = t.json('push', 'kimi', 'vps', 'devbox', '--json')
    assert.deepEqual(r.results.map(x => x.status), ['pushed', 'pushed'])
    assert.ok(t.readRemoteProfile('vps', 'kimi'))
    assert.ok(t.readRemoteProfile('devbox', 'kimi'))
    assert.deepEqual(t.readLocalConfig().profiles.kimi.remotes, ['devbox', 'vps'])
    // one multi-target push ships the SAME unit (full membership) to every target
    const vpsCfg = JSON.parse(fs.readFileSync(path.join(t.tmp, 'vps', 'config.json'), 'utf8'))
    assert.deepEqual(vpsCfg.profiles.kimi.remotes, ['devbox', 'vps'])
    // one remote moves: the remotes now disagree → sync conflicts on both,
    // it must NOT pull one and propagate to the other
    t.writeRemoteProfile('devbox', 'kimi', { model: 'devbox-take' })
    const conflict = t.agentswap('sync', '--json')
    assert.equal(conflict.code, 1)
    assert.match(conflict.out, /remotes disagree/)
    // explicit pull from one remote resolves it
    const ok = t.json('pull', 'kimi', 'devbox', '--json')
    assert.equal(ok.results[0].status, 'pulled')
    assert.equal(t.readLocalProfile('kimi').model, 'devbox-take')
  } finally { t.cleanup() }
})

test('a multi-target push leaves every remote in sync', { skip }, () => {
  const t = setup(['vps', 'devbox'])
  try {
    t.agentswap('add', 'kimi')
    t.json('push', 'kimi', 'vps', 'devbox', '--json')
    const r = t.json('sync', '--json')
    assert.deepEqual(r.results.map(x => x.status), ['in-sync', 'in-sync'])
  } finally { t.cleanup() }
})

test('adding a second remote later converges instead of conflicting', { skip }, () => {
  const t = setup(['vps', 'devbox'])
  try {
    t.agentswap('add', 'kimi')
    t.json('push', 'kimi', 'vps', '--json')
    // devbox gets membership [devbox, vps]; vps still holds [vps]. Neither
    // remote moved since this machine synced it — only this machine did.
    t.json('push', 'kimi', 'devbox', '--json')
    const first = t.json('sync', '--json')
    const byRemote = Object.fromEntries(first.results.map(x => [x.remote, x.status]))
    assert.deepEqual(byRemote, { vps: 'pushed', devbox: 'in-sync' })
    const second = t.json('sync', '--json')
    assert.deepEqual(second.results.map(x => x.status), ['in-sync', 'in-sync'])
  } finally { t.cleanup() }
})

test('a remote profile whose name only differs in case from a local one is never pulled over it', { skip }, () => {
  const t = setup()
  try {
    t.writeLocalProfile('work', { model: 'local' })
    t.writeRemoteProfile('vps', 'Work', { model: 'remote' })
    const r = t.agentswap('sync', '--json')
    assert.equal(r.code, 1)
    const res = JSON.parse(r.out).results.find(x => x.name === 'Work')
    assert.equal(res.status, 'name-collision')
    assert.deepEqual(t.readLocalProfile('work'), { model: 'local' })
    assert.ok(!fs.readdirSync(path.join(t.local, 'profiles')).includes('Work.json'))
  } finally { t.cleanup() }
})

test('pushing a name that only differs in case from a remote profile is refused by the remote', { skip }, () => {
  const t = setup()
  try {
    t.writeLocalProfile('Work', { model: 'local' })
    t.writeRemoteProfile('vps', 'work', { model: 'remote' })
    const r = t.agentswap('push', 'Work', 'vps', '--force', '--json')
    assert.equal(r.code, 1)
    assert.equal(JSON.parse(r.out).results[0].status, 'name-collision')
    assert.deepEqual(t.readRemoteProfile('vps', 'work'), { model: 'remote' })
  } finally { t.cleanup() }
})

test('meta-only changes move the sync unit: remote notes edit is pulled', { skip }, () => {
  const t = setup()
  try {
    t.agentswap('add', 'kimi')
    t.json('push', 'kimi', 'vps', '--json')
    // edit ONLY the remote meta (notes), profile file untouched
    const remoteCfgPath = path.join(t.tmp, 'vps', 'config.json')
    const remoteCfg = JSON.parse(fs.readFileSync(remoteCfgPath, 'utf8'))
    remoteCfg.profiles.kimi.notes = 'edited-on-remote'
    fs.writeFileSync(remoteCfgPath, JSON.stringify(remoteCfg, null, 2))
    const r = t.json('sync', '--json')
    assert.equal(r.results[0].status, 'pulled')
    assert.equal(t.readLocalConfig().profiles.kimi.notes, 'edited-on-remote')
  } finally { t.cleanup() }
})

test('pull of a missing profile exits non-zero', { skip }, () => {
  const t = setup()
  try {
    const r = t.agentswap('pull', 'ghost', 'vps', '--json')
    assert.equal(r.code, 1)
    assert.equal(JSON.parse(r.out).results[0].status, 'remote-missing')
  } finally { t.cleanup() }
})

test('pull refuses when member remotes disagree; explicit single remote works', { skip }, () => {
  const t = setup(['vps', 'devbox'])
  try {
    t.agentswap('add', 'kimi')
    t.json('push', 'kimi', 'vps', 'devbox', '--json')
    // devbox moves ahead; vps stays at the pushed baseline → remotes disagree
    t.writeRemoteProfile('devbox', 'kimi', { model: 'devbox-take' })
    const bad = t.agentswap('pull', 'kimi', '--json')
    assert.equal(bad.code, 1)
    assert.match(bad.out, /remotes disagree/)
    const ok = t.json('pull', 'kimi', 'devbox', '--json')
    assert.equal(ok.results[0].status, 'pulled')
    assert.equal(t.readLocalProfile('kimi').model, 'devbox-take')
  } finally { t.cleanup() }
})

test('remote change between list and write is a conflict, not a clobber', { skip }, () => {
  const t = setup()
  try {
    t.agentswap('add', 'kimi')
    t.json('push', 'kimi', 'vps', '--json')
    // local moves ahead of the remote...
    t.writeLocalProfile('kimi', { model: 'local-take' })
    // ...and the remote ALSO moves, after our list would have observed it.
    // Simulate by editing the remote, then forcing a stale-CAS write:
    // push with force off must refuse (conflict), because the remote hash no
    // longer matches the baseline either side agrees on.
    t.writeRemoteProfile('vps', 'kimi', { model: 'remote-take' })
    const r = t.agentswap('push', 'kimi', 'vps', '--json')
    assert.equal(r.code, 1)
    assert.match(r.out, /conflict/)
    assert.equal(t.readRemoteProfile('vps', 'kimi').model, 'remote-take')
  } finally { t.cleanup() }
})
