import { describe, test, before, after } from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import http from 'node:http'
import { fileURLToPath } from 'node:url'
import { createWebServer } from '../src/web/server.mjs'
import { addRemote } from '../src/remotes.mjs'
import { installFakeSsh } from './helpers/fake-ssh.mjs'

// The web remotes API drives the SAME __sync protocol the CLI uses, over a
// fake ssh that forwards to a second agentswap home. POSIX-only (the shim is
// an extensionless executable); transport behavior on Windows is covered by
// the ssh unit tests.

const here = path.dirname(fileURLToPath(import.meta.url))
const bin = path.join(here, '..', 'bin', 'agentswap.mjs')
const skip = process.platform === 'win32'

describe('web remotes API', { skip }, () => {
  let tmp, local, remoteHome, server, url, token

  before(async () => {
    tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'agentswap-web-remotes-'))
    local = path.join(tmp, 'local')
    remoteHome = path.join(tmp, 'vps')
    fs.mkdirSync(path.join(local, 'profiles'), { recursive: true })
    fs.mkdirSync(path.join(remoteHome, 'profiles'), { recursive: true })
    const sshDir = path.join(tmp, 'bin')
    installFakeSsh(sshDir, { vps: remoteHome })
    process.env.AGENTSWAP_HOME = local
    process.env.PATH = `${sshDir}${path.delimiter}${process.env.PATH}`

    // A profile on the remote that already carries sync membership in its
    // meta — the round-trip tests must not strip it.
    fs.writeFileSync(path.join(remoteHome, 'profiles', 'p1.json'), JSON.stringify({
      model: 'm1', env: { ANTHROPIC_AUTH_TOKEN: 'sk-remote-secret' },
    }, null, 2))
    fs.writeFileSync(path.join(remoteHome, 'config.json'), JSON.stringify({
      profiles: { p1: { remotes: ['vps'] } },
    }, null, 2))

    const r = await addRemote('vps', { host: 'vps', bin })
    assert.equal(r.exitCode, 0, r.output)

    const created = await createWebServer({ port: 0 })
    server = created.server
    url = created.url
    token = created.token
  })

  after(async () => {
    await new Promise((resolve) => server.close(resolve))
    fs.rmSync(tmp, { recursive: true, force: true })
  })

  function sendJson(method, route, body) {
    return new Promise((resolve, reject) => {
      const data = body ? JSON.stringify(body) : ''
      const req = http.request(url + route, {
        method,
        headers: {
          'x-agentswap-token': token,
          ...(data ? { 'content-type': 'application/json', 'content-length': Buffer.byteLength(data) } : {}),
        },
      }, (res) => {
        let b = ''
        res.on('data', (c) => { b += c })
        res.on('end', () => resolve({ status: res.statusCode, body: b }))
      })
      req.on('error', reject)
      req.end(data)
    })
  }

  test('GET /api/remotes lists registered remotes with their profiles', async () => {
    const r = await sendJson('GET', '/api/remotes')
    assert.equal(r.status, 200, r.body)
    const body = JSON.parse(r.body)
    assert.equal(body.version, 2)
    assert.equal(body.remotes.length, 1)
    const vps = body.remotes[0]
    assert.equal(vps.name, 'vps')
    assert.equal(vps.ok, true)
    assert.ok(vps.profiles.some((p) => p.name === 'p1' && typeof p.hash === 'string'))
  })

  test('GET reads a remote profile with meta and sync-unit hash', async () => {
    const r = await sendJson('GET', '/api/remotes/vps/profiles/p1')
    assert.equal(r.status, 200, r.body)
    const body = JSON.parse(r.body)
    assert.equal(body.settings.model, 'm1')
    assert.deepEqual(body.meta.remotes, ['vps'])
    assert.ok(typeof body.hash === 'string' && body.hash.length > 0)
  })

  test('PUT CAS-writes the remote profile and preserves membership', async () => {
    const read = JSON.parse((await sendJson('GET', '/api/remotes/vps/profiles/p1')).body)
    const settings = { ...read.settings, model: 'm2' }
    const r = await sendJson('PUT', '/api/remotes/vps/profiles/p1', {
      settings, notes: 'tweaked', launchArgs: null, unsetEnv: [],
      remotes: read.meta.remotes, expectedHash: read.hash,
    })
    assert.equal(r.status, 200, r.body)
    const written = JSON.parse(fs.readFileSync(path.join(remoteHome, 'profiles', 'p1.json'), 'utf8'))
    assert.equal(written.model, 'm2')
    const meta = JSON.parse(fs.readFileSync(path.join(remoteHome, 'config.json'), 'utf8')).profiles.p1
    assert.deepEqual(meta.remotes, ['vps'])
    assert.equal(meta.notes, 'tweaked')
  })

  test('PUT with a stale hash is a conflict, not an overwrite', async () => {
    const r = await sendJson('PUT', '/api/remotes/vps/profiles/p1', {
      settings: { model: 'm3' }, expectedHash: 'deadbeef-stale',
    })
    assert.equal(r.status, 409)
    assert.match(r.body, /remote-conflict/)
    const written = JSON.parse(fs.readFileSync(path.join(remoteHome, 'profiles', 'p1.json'), 'utf8'))
    assert.equal(written.model, 'm2', 'the stale write must not have landed')
  })

  test('PUT with force overwrites despite a stale hash', async () => {
    const r = await sendJson('PUT', '/api/remotes/vps/profiles/p1', {
      settings: { model: 'm4' }, expectedHash: 'deadbeef-stale', force: true,
    })
    assert.equal(r.status, 200, r.body)
    const written = JSON.parse(fs.readFileSync(path.join(remoteHome, 'profiles', 'p1.json'), 'utf8'))
    assert.equal(written.model, 'm4')
  })

  test('PUT without expectedHash is rejected (no silent force)', async () => {
    const r = await sendJson('PUT', '/api/remotes/vps/profiles/p1', { settings: { model: 'm5' } })
    assert.equal(r.status, 400)
    assert.match(r.body, /expected-hash-required/)
  })

  test('PUT rejects an invalid profile before touching the remote', async () => {
    const read = JSON.parse((await sendJson('GET', '/api/remotes/vps/profiles/p1')).body)
    const r = await sendJson('PUT', '/api/remotes/vps/profiles/p1', {
      settings: ['not', 'an', 'object'], expectedHash: read.hash,
    })
    assert.equal(r.status, 400)
    assert.match(r.body, /invalid-profile/)
  })

  test('an unknown remote name is 404', async () => {
    const r = await sendJson('GET', '/api/remotes/nope/profiles/p1')
    assert.equal(r.status, 404)
    assert.match(r.body, /remote-not-found/)
  })

  test('POST registers a remote after a successful probe', async () => {
    const r = await sendJson('POST', '/api/remotes', { name: 'vps2', host: 'vps', bin })
    assert.equal(r.status, 201, r.body)
    const body = JSON.parse(r.body)
    assert.equal(body.name, 'vps2')
    const listed = JSON.parse((await sendJson('GET', '/api/remotes')).body)
    assert.ok(listed.remotes.some((x) => x.name === 'vps2' && x.ok))
  })

  test('POST a duplicate name conflicts', async () => {
    const r = await sendJson('POST', '/api/remotes', { name: 'vps2', host: 'vps', bin })
    assert.equal(r.status, 409)
    assert.match(r.body, /remote-exists/)
  })

  test('POST an invalid name is rejected', async () => {
    const r = await sendJson('POST', '/api/remotes', { name: '../evil', host: 'vps' })
    assert.equal(r.status, 400)
    assert.match(r.body, /invalid-remote/)
  })

  test('POST an unreachable host fails the probe and is refused', async () => {
    const r = await sendJson('POST', '/api/remotes', { name: 'offline', host: 'unknown-host', bin })
    assert.equal(r.status, 502)
    assert.match(r.body, /add-failed/)
    const listed = JSON.parse((await sendJson('GET', '/api/remotes')).body)
    assert.ok(!listed.remotes.some((x) => x.name === 'offline'), 'a failed probe must not register the remote')
  })

  test('DELETE unregisters a remote', async () => {
    const r = await sendJson('DELETE', '/api/remotes/vps2')
    assert.equal(r.status, 200, r.body)
    const listed = JSON.parse((await sendJson('GET', '/api/remotes')).body)
    assert.ok(!listed.remotes.some((x) => x.name === 'vps2'))
  })

  test('DELETE an unknown remote is 404', async () => {
    const r = await sendJson('DELETE', '/api/remotes/nope')
    assert.equal(r.status, 404)
    assert.match(r.body, /remote-not-found/)
  })

  test('an unreachable remote is reported, not fatal to the others', async () => {
    // Register a ghost directly in config — addRemote would probe-fail and
    // refuse to register it. The fake ssh exits 255 on unknown hosts.
    const cfgPath = path.join(local, 'config.json')
    const cfg = JSON.parse(fs.readFileSync(cfgPath, 'utf8'))
    cfg.remotes.ghost = { host: 'unknown-host', bin }
    fs.writeFileSync(cfgPath, JSON.stringify(cfg, null, 2))

    const r = await sendJson('GET', '/api/remotes')
    assert.equal(r.status, 200, r.body)
    const body = JSON.parse(r.body)
    const ghost = body.remotes.find((x) => x.name === 'ghost')
    assert.equal(ghost.ok, false)
    assert.ok(ghost.error)
    const vps = body.remotes.find((x) => x.name === 'vps')
    assert.equal(vps.ok, true)
  })
})
