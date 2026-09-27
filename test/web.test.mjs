import { describe, test, before, after } from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import http from 'node:http'
import { createWebServer } from '../src/web/server.mjs'
import { configDirFingerprint } from '../src/store.mjs'
import { BUILTIN_HOOKS, HOOK_PLATFORMS } from '../src/web/hook-library.mjs'
import { normalize } from '../src/web/hook-store.mjs'

/**
 * The bundled presets are data, and data rots silently: a typo in an event
 * name or a missing platform variant only shows up when someone tries to use
 * that hook. Hold them to the same contract the API enforces on saved ones.
 */
describe('bundled hook presets', () => {
  test('every preset passes the same validation a saved one does', () => {
    for (const p of BUILTIN_HOOKS) {
      const v = normalize(p)
      assert.ok(v.ok, `${p.id}: ${v.reason}`)
    }
  })

  test('ids are unique and every command targets a known platform', () => {
    const ids = BUILTIN_HOOKS.map((p) => p.id)
    assert.equal(new Set(ids).size, ids.length, 'duplicate preset id')
    for (const p of BUILTIN_HOOKS) {
      assert.ok(/^[a-z0-9-]+$/.test(p.id), `${p.id}: id must be a slug`)
      for (const plat of Object.keys(p.commands)) {
        assert.ok(HOOK_PLATFORMS.includes(plat), `${p.id}: unknown platform ${plat}`)
        assert.ok(p.commands[plat].trim().length > 0, `${p.id}: empty ${plat} command`)
      }
      assert.equal(typeof p.name.en, 'string')
      assert.equal(typeof p.name.zh, 'string')
      assert.equal(typeof p.description.en, 'string')
      assert.equal(typeof p.description.zh, 'string')
    }
  })

  test('a blocking preset exits 2 and a non-blocking one never does', () => {
    for (const p of BUILTIN_HOOKS) {
      for (const cmd of Object.values(p.commands)) {
        const exits2 = /exit 2\b/.test(cmd)
        assert.equal(exits2, p.blocking === true,
          `${p.id}: blocking=${!!p.blocking} but the command ${exits2 ? 'does' : 'does not'} exit 2`)
      }
    }
  })
})

/**
 * Web server security contract. The server binds loopback, but the token and
 * Host checks are what actually stop DNS-rebinding and local-process reads.
 */
describe('web server', () => {
  let tmp, appHome, claudeDir, server, url, token

  before(async () => {
    tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'agentswap-web-'))
    appHome = path.join(tmp, 'agentswap-home')
    claudeDir = path.join(tmp, 'claude')
    process.env.AGENTSWAP_HOME = appHome
    process.env.CLAUDE_CONFIG_DIR = claudeDir
    fs.mkdirSync(claudeDir, { recursive: true })
    fs.writeFileSync(path.join(claudeDir, 'settings.json'), JSON.stringify({
      model: 'm1',
      env: { ANTHROPIC_AUTH_TOKEN: 'sk-web-secret-1234567890' },
    }))
    const created = await createWebServer({ port: 0 })
    server = created.server
    url = created.url
    token = created.token
  })

  after(async () => {
    await new Promise((resolve) => server.close(resolve))
    fs.rmSync(tmp, { recursive: true, force: true })
  })

  function rawRequest(headers = {}) {
    return new Promise((resolve, reject) => {
      const req = http.get(url + '/api/state', { headers }, (res) => {
        let body = ''
        res.on('data', (c) => { body += c })
        res.on('end', () => resolve({ status: res.statusCode, body }))
      })
      req.on('error', reject)
    })
  }

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

  test('API rejects requests without the token (reads included)', async () => {
    const r = await rawRequest()
    assert.equal(r.status, 403)
    assert.match(r.body, /bad-token/)
  })

  test('API rejects a wrong token', async () => {
    const r = await rawRequest({ 'x-agentswap-token': 'nope' })
    assert.equal(r.status, 403)
  })

  test('API serves state with the correct token', async () => {
    const r = await rawRequest({ 'x-agentswap-token': token })
    assert.equal(r.status, 200)
    const body = JSON.parse(r.body)
    assert.equal(body.version, 2)
    assert.ok(Array.isArray(body.profiles))
  })

  test('Host header must be loopback (DNS-rebinding guard)', async () => {
    const r = await rawRequest({ 'x-agentswap-token': token, Host: 'localhost.evil.com' })
    assert.equal(r.status, 403)
    assert.match(r.body, /forbidden-host/)
  })

  test('the index page does not embed the token', async () => {
    const r = await new Promise((resolve, reject) => {
      http.get(url + '/', (res) => {
        let body = ''
        res.on('data', (c) => { body += c })
        res.on('end', () => resolve({ status: res.statusCode, body }))
      }).on('error', reject)
    })
    assert.equal(r.status, 200)
    assert.ok(!r.body.includes(token), 'token leaked into the page HTML')
  })

  test('encoded-slash profile names are rejected', async () => {
    const r = await new Promise((resolve, reject) => {
      http.get(url + '/api/profiles/..%2Fconfig', { headers: { 'x-agentswap-token': token } }, (res) => {
        let body = ''
        res.on('data', (c) => { body += c })
        res.on('end', () => resolve({ status: res.statusCode, body }))
      }).on('error', reject)
    })
    assert.equal(r.status, 400)
    assert.match(r.body, /name-invalid/)
  })

  test('same-origin mutations with a browser Origin header are accepted', async () => {
    const body = JSON.stringify({ name: 'origin-test' })
    const r = await new Promise((resolve, reject) => {
      const req = http.request(url + '/api/profiles', {
        method: 'POST',
        headers: {
          'x-agentswap-token': token,
          Origin: url, // browsers send http://127.0.0.1:<port> on same-origin POSTs
          'content-type': 'application/json',
          'content-length': Buffer.byteLength(body),
        },
      }, (res) => {
        let b = ''
        res.on('data', (c) => { b += c })
        res.on('end', () => resolve({ status: res.statusCode, body: b }))
      })
      req.on('error', reject)
      req.end(body)
    })
    assert.equal(r.status, 201, r.body)
  })

  test('mutations with a non-loopback Origin are blocked even with the token', async () => {
    const body = JSON.stringify({ name: 'evil-origin' })
    const r = await new Promise((resolve, reject) => {
      const req = http.request(url + '/api/profiles', {
        method: 'POST',
        headers: {
          'x-agentswap-token': token,
          Origin: 'http://evil.example.com:47831',
          'content-type': 'application/json',
          'content-length': Buffer.byteLength(body),
        },
      }, (res) => {
        let b = ''
        res.on('data', (c) => { b += c })
        res.on('end', () => resolve({ status: res.statusCode, body: b }))
      })
      req.on('error', reject)
      req.end(body)
    })
    assert.equal(r.status, 403)
    assert.match(r.body, /forbidden-origin/)
  })

  test('state written for another config dir is not trusted (no ghost active profile)', async () => {
    fs.mkdirSync(path.join(appHome, 'profiles'), { recursive: true })
    fs.writeFileSync(path.join(appHome, 'profiles', 'p1.json'), JSON.stringify({ model: 'm' }))
    const stateFile = path.join(appHome, 'state.json')
    fs.writeFileSync(stateFile, JSON.stringify({
      version: 2, activeProfile: 'p1', appliedHash: 'x', configDirFingerprint: 'deadbeefdead',
    }))
    let r = await rawRequest({ 'x-agentswap-token': token })
    let body = JSON.parse(r.body)
    assert.equal(body.active, null)
    assert.ok(!body.profiles.some((p) => p.active), 'a foreign-fingerprint state marked a profile active')

    // a state with NO fingerprint at all is equally untrusted
    fs.writeFileSync(stateFile, JSON.stringify({ version: 2, activeProfile: 'p1', appliedHash: 'x' }))
    r = await rawRequest({ 'x-agentswap-token': token })
    body = JSON.parse(r.body)
    assert.equal(body.active, null)

    // the correct fingerprint restores trust
    fs.writeFileSync(stateFile, JSON.stringify({
      version: 2, activeProfile: 'p1', appliedHash: 'x', configDirFingerprint: configDirFingerprint(),
    }))
    r = await rawRequest({ 'x-agentswap-token': token })
    body = JSON.parse(r.body)
    assert.equal(body.active, 'p1')
    fs.rmSync(stateFile, { force: true })
  })

  test('DELETE removes a profile and its metadata; clears active; 404 when missing', async () => {
    await sendJson('POST', '/api/profiles', { name: 'del-a', notes: 'n' })
    let r = await sendJson('DELETE', '/api/profiles/del-a')
    assert.equal(r.status, 200, r.body)
    assert.equal(JSON.parse(r.body).clearedActive, false)
    assert.ok(!fs.existsSync(path.join(appHome, 'profiles', 'del-a.json')))
    assert.equal(JSON.parse(fs.readFileSync(path.join(appHome, 'config.json'), 'utf8')).profiles['del-a'], undefined)

    await sendJson('POST', '/api/profiles', { name: 'del-active' })
    assert.equal((await sendJson('POST', '/api/switch', { name: 'del-active', discard: true })).status, 200)
    r = await sendJson('DELETE', '/api/profiles/del-active')
    assert.equal(r.status, 200, r.body)
    assert.equal(JSON.parse(r.body).clearedActive, true)
    assert.equal(JSON.parse((await rawRequest({ 'x-agentswap-token': token })).body).active, null)

    assert.equal((await sendJson('DELETE', '/api/profiles/del-a')).status, 404)
  })

  test('renaming a profile moves its file and metadata', async () => {
    await sendJson('POST', '/api/profiles', { name: 'rn-a' })
    await sendJson('PUT', '/api/profiles/rn-a', {
      settings: { model: 'm' }, notes: 'note-a', launchArgs: null, unsetEnv: [],
    })
    const r = await sendJson('POST', '/api/profiles/rn-a/rename', { newName: 'rn-b' })
    assert.equal(r.status, 200, r.body)
    assert.equal(JSON.parse(r.body).name, 'rn-b')

    const gone = await sendJson('GET', '/api/profiles/rn-a')
    assert.equal(gone.status, 404)
    const got = await sendJson('GET', '/api/profiles/rn-b')
    assert.equal(got.status, 200)
    assert.equal(JSON.parse(got.body).notes, 'note-a')
  })

  test('renaming an active profile moves the active pointer', async () => {
    await sendJson('POST', '/api/profiles', { name: 'rn-active' })
    const sw = await sendJson('POST', '/api/switch', { name: 'rn-active', discard: true })
    assert.equal(sw.status, 200, sw.body)
    const r = await sendJson('POST', '/api/profiles/rn-active/rename', { newName: 'rn-active2' })
    assert.equal(r.status, 200, r.body)
    const st = await rawRequest({ 'x-agentswap-token': token })
    assert.equal(JSON.parse(st.body).active, 'rn-active2')
  })

  test('renaming a synced profile is refused (the next sync would pull the old name back)', async () => {
    // membership: the profile is kept in sync with a remote
    await sendJson('POST', '/api/profiles', { name: 'rn-member' })
    const cfgPath = path.join(appHome, 'config.json')
    const cfg = JSON.parse(fs.readFileSync(cfgPath, 'utf8'))
    cfg.profiles['rn-member'].remotes = ['vps']
    fs.writeFileSync(cfgPath, JSON.stringify(cfg))
    let r = await sendJson('POST', '/api/profiles/rn-member/rename', { newName: 'rn-member2' })
    assert.equal(r.status, 409, r.body)
    assert.equal(JSON.parse(r.body).error, 'profile-synced')
    assert.match(JSON.parse(r.body).detail, /vps/)
    assert.equal((await sendJson('GET', '/api/profiles/rn-member')).status, 200, 'nothing moved')

    // a sync baseline alone (no membership) also means a remote holds a copy
    await sendJson('POST', '/api/profiles', { name: 'rn-baselined' })
    const syncPath = path.join(appHome, 'sync.json')
    fs.writeFileSync(syncPath, JSON.stringify({ version: 1, remotes: { devbox: { profiles: { 'rn-baselined': { hash: 'h' } } } } }))
    try {
      r = await sendJson('POST', '/api/profiles/rn-baselined/rename', { newName: 'rn-baselined2' })
      assert.equal(r.status, 409, r.body)
      assert.match(JSON.parse(r.body).detail, /devbox/)
      assert.equal((await sendJson('GET', '/api/profiles/rn-baselined2')).status, 404)
    } finally {
      fs.rmSync(syncPath, { force: true })
    }
  })

  test('renaming never overwrites a profile whose name differs only in case', async () => {
    await sendJson('POST', '/api/profiles', { name: 'rn-case-src' })
    await sendJson('POST', '/api/profiles', { name: 'rn-case-dst' })
    const dir = path.join(appHome, 'profiles')
    const before = fs.readFileSync(path.join(dir, 'rn-case-dst.json'), 'utf8')
    const r = await sendJson('POST', '/api/profiles/rn-case-src/rename', { newName: 'RN-CASE-DST' })
    // one file on macOS/Windows; refused on Linux too, so names stay portable
    assert.equal(r.status, 409, r.body)
    assert.equal(fs.readFileSync(path.join(dir, 'rn-case-dst.json'), 'utf8'), before, 'the other profile must survive')
  })

  test('a case-only rename of the same profile works on every platform', async () => {
    await sendJson('POST', '/api/profiles', { name: 'rn-caps' })
    const r = await sendJson('POST', '/api/profiles/rn-caps/rename', { newName: 'RN-CAPS' })
    assert.equal(r.status, 200, r.body)
    const names = fs.readdirSync(path.join(appHome, 'profiles'))
    assert.ok(names.includes('RN-CAPS.json') && !names.includes('rn-caps.json'), names.join(','))
  })

  test('renaming to an existing name conflicts', async () => {
    await sendJson('POST', '/api/profiles', { name: 'rn-x' })
    await sendJson('POST', '/api/profiles', { name: 'rn-y' })
    const r = await sendJson('POST', '/api/profiles/rn-x/rename', { newName: 'rn-y' })
    assert.equal(r.status, 409)
  })

  test('renaming rejects invalid and reserved names', async () => {
    await sendJson('POST', '/api/profiles', { name: 'rn-z' })
    const bad = await sendJson('POST', '/api/profiles/rn-z/rename', { newName: 'a b' })
    assert.equal(bad.status, 400)
    const reserved = await sendJson('POST', '/api/profiles/rn-z/rename', { newName: 'use' })
    assert.equal(reserved.status, 400)
  })

  test('local profile saves are compare-and-swap on expectedHash', async () => {
    await sendJson('POST', '/api/profiles', { name: 'cas-p' })
    const got = await sendJson('GET', '/api/profiles/cas-p')
    assert.equal(got.status, 200)
    const { hash } = JSON.parse(got.body)
    assert.equal(typeof hash, 'string')

    // stale hash → 409 with the current hash, file untouched
    const stale = await sendJson('PUT', '/api/profiles/cas-p', {
      settings: { model: 'stale' }, expectedHash: 'not-the-hash',
    })
    assert.equal(stale.status, 409)
    assert.equal(JSON.parse(stale.body).error, 'profile-conflict')
    assert.equal(JSON.parse(stale.body).hash, hash)
    const untouched = await sendJson('GET', '/api/profiles/cas-p')
    assert.equal(JSON.parse(untouched.body).hash, hash)

    // matching hash → write lands and the new hash is returned
    const okPut = await sendJson('PUT', '/api/profiles/cas-p', {
      settings: { model: 'v2' }, expectedHash: hash,
    })
    assert.equal(okPut.status, 200, okPut.body)
    const newHash = JSON.parse(okPut.body).hash
    assert.equal(typeof newHash, 'string')
    assert.notEqual(newHash, hash)

    // no expectedHash → force path (legacy behavior) still writes
    const force = await sendJson('PUT', '/api/profiles/cas-p', {
      settings: { model: 'v3' },
    })
    assert.equal(force.status, 200)
    const after = await sendJson('GET', '/api/profiles/cas-p')
    assert.equal(JSON.parse(after.body).settings.model, 'v3')
  })

  test('the hook library serves built-ins and round-trips a saved preset', async () => {
    const listed = await sendJson('GET', '/api/hooks')
    assert.equal(listed.status, 200)
    const lib = JSON.parse(listed.body)
    assert.equal(lib.version, 2)
    assert.equal(typeof lib.platform, 'string')
    const builtin = lib.presets.find((p) => p.id === 'guard-secret-files')
    assert.ok(builtin, 'the bundled presets are served')
    assert.equal(builtin.builtin, true)
    assert.ok(builtin.commands.darwin && builtin.commands.win32, 'presets carry per-platform commands')

    const created = await sendJson('POST', '/api/hooks', {
      name: 'my hook', event: 'Stop', commands: { darwin: 'echo hi', win32: 'Write-Output hi' },
      matcher: '*', timeout: 5, async: true,
    })
    assert.equal(created.status, 201, created.body)
    const id = JSON.parse(created.body).preset.id
    assert.ok(id.startsWith('u-'))

    const after = JSON.parse((await sendJson('GET', '/api/hooks')).body)
    const mine = after.presets.find((p) => p.id === id)
    assert.equal(mine.builtin, false)
    assert.equal(mine.commands.darwin, 'echo hi')
    assert.equal(mine.async, true)

    const del = await sendJson('DELETE', '/api/hooks/' + id)
    assert.equal(del.status, 200)
    const gone = JSON.parse((await sendJson('GET', '/api/hooks')).body)
    assert.ok(!gone.presets.some((p) => p.id === id))
  })

  test('the hook library refuses malformed presets and built-in deletes', async () => {
    const noName = await sendJson('POST', '/api/hooks', { event: 'Stop', commands: { darwin: 'x' } })
    assert.equal(noName.status, 400)
    assert.match(noName.body, /invalid-preset/)

    const noCommand = await sendJson('POST', '/api/hooks', { name: 'x', event: 'Stop', commands: {} })
    assert.equal(noCommand.status, 400)

    const badEvent = await sendJson('POST', '/api/hooks', { name: 'x', event: 'not an event', commands: { darwin: 'x' } })
    assert.equal(badEvent.status, 400)

    const builtin = await sendJson('DELETE', '/api/hooks/guard-secret-files')
    assert.equal(builtin.status, 403)
    assert.match(builtin.body, /preset-builtin/)

    const missing = await sendJson('DELETE', '/api/hooks/u-does-not-exist')
    assert.equal(missing.status, 404)
  })

  /**
   * The library is a file the page has to load on every visit. A preset that
   * can be arbitrarily large turns a shelf into a denial of service against
   * the editor itself, so every text field is capped and so is the whole.
   */
  test('the hook library caps what one preset can carry', async () => {
    const big = 'x'.repeat(9000)
    const longCommand = await sendJson('POST', '/api/hooks', {
      name: 'n', event: 'Stop', commands: { darwin: big },
    })
    assert.equal(longCommand.status, 400)
    assert.match(longCommand.body, /too long/)

    const longName = await sendJson('POST', '/api/hooks', {
      name: 'x'.repeat(400), event: 'Stop', commands: { darwin: 'x' },
    })
    assert.equal(longName.status, 400)

    const longLocalizedName = await sendJson('POST', '/api/hooks', {
      name: { en: 'x'.repeat(400) }, event: 'Stop', commands: { darwin: 'x' },
    })
    assert.equal(longLocalizedName.status, 400)

    const blankName = await sendJson('POST', '/api/hooks', {
      name: { en: '   ' }, event: 'Stop', commands: { darwin: 'x' },
    })
    assert.equal(blankName.status, 400)

    const fatDescription = await sendJson('POST', '/api/hooks', {
      name: 'n', event: 'Stop', commands: { darwin: 'x' }, description: { en: 'y'.repeat(4000) },
    })
    assert.equal(fatDescription.status, 400)

    // a localized map with junk keys is not a description
    const junkKeys = await sendJson('POST', '/api/hooks', {
      name: 'n', event: 'Stop', commands: { darwin: 'x' }, description: { 'not a lang': 'y' },
    })
    assert.equal(junkKeys.status, 400)

    // a valid bilingual preset still goes through
    const ok = await sendJson('POST', '/api/hooks', {
      name: { en: 'fine', zh: '可以' }, event: 'Stop', commands: { darwin: 'x' },
      requires: ['jq', 42, 'z'.repeat(200)],
    })
    assert.equal(ok.status, 201, ok.body)
    const saved = JSON.parse(ok.body).preset
    assert.deepEqual(saved.requires, ['jq'], 'non-strings and over-long entries are dropped')
    await sendJson('DELETE', '/api/hooks/' + saved.id)
  })

  test('a corrupt hook library reads as empty instead of failing the page', async () => {
    const libPath = path.join(appHome, 'hooks.json')
    fs.writeFileSync(libPath, '{ this is not json')
    const r = await sendJson('GET', '/api/hooks')
    assert.equal(r.status, 200)
    const body = JSON.parse(r.body)
    assert.ok(body.presets.length > 0, 'the bundled presets still load')
    assert.ok(!body.presets.some((p) => !p.builtin), 'nothing is read out of the corrupt file')
    fs.rmSync(libPath, { force: true })
  })

  test('the CAS hash covers meta, and web saves keep sync membership', async () => {
    await sendJson('POST', '/api/profiles', { name: 'cas-m' })
    // plant sync membership the editor never carries
    const cfgPath = path.join(appHome, 'config.json')
    const cfg = JSON.parse(fs.readFileSync(cfgPath, 'utf8'))
    cfg.profiles['cas-m'].remotes = ['vps1']
    fs.writeFileSync(cfgPath, JSON.stringify(cfg))

    const g1 = await sendJson('GET', '/api/profiles/cas-m')
    const h1 = JSON.parse(g1.body).hash

    // meta-only change (notes) must move the hash…
    const put = await sendJson('PUT', '/api/profiles/cas-m', {
      settings: {}, notes: 'meta only', expectedHash: h1,
    })
    assert.equal(put.status, 200, put.body)
    const h2 = JSON.parse(put.body).hash
    assert.notEqual(h2, h1)

    // …so a second tab still holding h1 conflicts even though settings match
    const stale = await sendJson('PUT', '/api/profiles/cas-m', {
      settings: {}, notes: 'other tab', expectedHash: h1,
    })
    assert.equal(stale.status, 409)

    // and the save must not have stripped the profile's sync membership
    const cfgAfter = JSON.parse(fs.readFileSync(cfgPath, 'utf8'))
    assert.deepEqual(cfgAfter.profiles['cas-m'].remotes, ['vps1'])
  })

  test('GET /api/templates returns version:2, current platform and all 3 templates', async () => {
    const res = await sendJson('GET', '/api/templates')
    assert.equal(res.status, 200)
    const body = JSON.parse(res.body)
    assert.equal(body.version, 2)
    assert.equal(body.platform, process.platform)
    assert.equal(body.templates.length, 3)
    const plats = body.templates.map(t => t.platform).sort()
    assert.deepEqual(plats, ['darwin', 'linux', 'win32'])
    for (const t of body.templates) {
      assert.ok(t.hash)
      assert.deepEqual(t.settings, { env: {} })
    }
  })

  test('PUT /api/templates/:platform updates template with CAS and rejects stale expectedHash with 409', async () => {
    const getRes = await sendJson('GET', '/api/templates')
    const darwin = JSON.parse(getRes.body).templates.find(t => t.platform === 'darwin')

    // Stale hash -> 409 template-conflict
    const conflictRes = await sendJson('PUT', '/api/templates/darwin', {
      settings: { env: { TEST: '1' } },
      expectedHash: 'stale-hash',
    })
    assert.equal(conflictRes.status, 409)
    assert.equal(JSON.parse(conflictRes.body).version, 2)
    assert.equal(JSON.parse(conflictRes.body).error, 'template-conflict')

    // Valid CAS -> 200 ok
    const putRes = await sendJson('PUT', '/api/templates/darwin', {
      settings: { env: { DARWIN_VAR: 'mac' } },
      expectedHash: darwin.hash,
    })
    assert.equal(putRes.status, 200)
    const putBody = JSON.parse(putRes.body)
    assert.equal(putBody.ok, true)
    assert.ok(putBody.hash)

    // Re-verify via GET
    const afterGet = await sendJson('GET', '/api/templates')
    const updatedDarwin = JSON.parse(afterGet.body).templates.find(t => t.platform === 'darwin')
    assert.deepEqual(updatedDarwin.settings, { env: { DARWIN_VAR: 'mac' } })
    assert.equal(updatedDarwin.hash, putBody.hash)
  })

  test('template validation failures have versioned envelopes and cannot change storage', async () => {
    const before = await sendJson('GET', '/api/templates')
    for (const [route, body] of [
      ['/api/templates/linux', { settings: { env: {} } }],
      ['/api/templates/linux', { expectedHash: 'stale' }],
      ['/api/templates/unknown', { settings: {}, expectedHash: 'stale' }],
    ]) {
      const res = await sendJson('PUT', route, body)
      assert.equal(res.status, 400)
      assert.equal(JSON.parse(res.body).version, 2)
    }
    assert.deepEqual(JSON.parse((await sendJson('GET', '/api/templates')).body), JSON.parse(before.body))
  })

  test('POST /api/profiles supports platform template and enforces mutual exclusivity', async () => {
    // 1. Create from explicit platform template
    const resPlat = await sendJson('POST', '/api/profiles', {
      name: 'from-darwin-tmpl',
      platform: 'darwin',
    })
    assert.equal(resPlat.status, 201)
    const profRes = await sendJson('GET', '/api/profiles/from-darwin-tmpl')
    const prof = JSON.parse(profRes.body)
    assert.deepEqual(prof.settings, { env: { DARWIN_VAR: 'mac' } })

    // 2. Mutual exclusivity conflict
    const conflictRes = await sendJson('POST', '/api/profiles', {
      name: 'conflict-p',
      fromCurrent: true,
      platform: 'linux',
    })
    assert.equal(conflictRes.status, 400)
    assert.equal(JSON.parse(conflictRes.body).error, 'source-conflict')

    // 3. Invalid platform rejected
    const badPlat = await sendJson('POST', '/api/profiles', {
      name: 'bad-plat-p',
      platform: 'freebsd',
    })
    assert.equal(badPlat.status, 400)
    assert.equal(JSON.parse(badPlat.body).error, 'invalid-platform')
  })
})

