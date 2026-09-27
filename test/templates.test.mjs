import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import os from 'node:os'
import {
  loadTemplates, saveTemplates, defaultTemplates,
  getTemplateForPlatform, listTemplatesSummary, updateTemplateCAS,
} from '../src/templates.mjs'
import { templatesPath } from '../src/paths.mjs'
import { contentHash } from '../src/settings.mjs'
import { withLock } from '../src/store.mjs'
import { createProfileCore } from '../src/commands/create-profile.mjs'

async function withTempAcsHome(fn) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'aswap-tmpl-test-'))
  const origHome = process.env.AGENTSWAP_HOME
  process.env.AGENTSWAP_HOME = dir
  try {
    return await fn(dir)
  } finally {
    if (origHome === undefined) delete process.env.AGENTSWAP_HOME
    else process.env.AGENTSWAP_HOME = origHome
    try { fs.rmSync(dir, { recursive: true, force: true }) } catch {}
  }
}

test('loadTemplates: returns defaults when file does not exist', async () => {
  await withTempAcsHome(async () => {
    const tmpls = loadTemplates()
    assert.deepEqual(tmpls, {
      darwin: { env: {} },
      linux: { env: {} },
      win32: { env: {} },
    })
  })
})

test('loadTemplates: fails closed (EINVALIDTEMPLATES) when a required platform is missing from existing file', async () => {
  await withTempAcsHome(async () => {
    fs.writeFileSync(templatesPath(), JSON.stringify({ darwin: { env: {} } }))
    assert.throws(() => loadTemplates(), { code: 'EINVALIDTEMPLATES' })
  })
})

test('loadTemplates: fails closed on unparseable JSON or non-object', async () => {
  await withTempAcsHome(async () => {
    fs.writeFileSync(templatesPath(), 'not valid json')
    assert.throws(() => loadTemplates(), { code: 'ETEMPLATESPARSE' })

    fs.writeFileSync(templatesPath(), '["array"]')
    assert.throws(() => loadTemplates(), { code: 'EINVALIDTEMPLATES' })
  })
})

test('loadTemplates & CAS: preserves unknown top-level fields across updates', async () => {
  await withTempAcsHome(async () => {
    const initial = {
      darwin: { env: { OS: 'mac' } },
      linux: { env: { OS: 'linux' } },
      win32: { env: { OS: 'win' } },
      customMeta: { note: 'keep me' },
    }
    saveTemplates(initial)

    const loaded = loadTemplates()
    assert.equal(loaded.customMeta.note, 'keep me')

    const dHash = contentHash({ env: { OS: 'mac' } })
    await withLock(() => {
      updateTemplateCAS('darwin', {
        settings: { env: { OS: 'macos-updated' } },
        expectedHash: dHash,
      })
    })

    const reloaded = loadTemplates()
    assert.equal(reloaded.darwin.env.OS, 'macos-updated')
    assert.equal(reloaded.customMeta.note, 'keep me')
  })
})

test('updateTemplateCAS: requires non-empty string expectedHash and enforces CAS conflict', async () => {
  await withTempAcsHome(async () => {
    const def = defaultTemplates()
    saveTemplates(def)

    // Missing expectedHash
    await assert.rejects(async () => {
      await withLock(() => updateTemplateCAS('linux', { settings: { env: { X: '1' } } }))
    }, { code: 'EMISSINGEXPECTEDHASH' })

    // Stale expectedHash
    await assert.rejects(async () => {
      await withLock(() => updateTemplateCAS('linux', {
        settings: { env: { X: '1' } },
        expectedHash: 'stale-hash',
      }))
    }, { code: 'ETEMPLATECONFLICT' })

    // Matching expectedHash
    const linuxHash = contentHash(def.linux)
    const res = await withLock(() => updateTemplateCAS('linux', {
      settings: { env: { X: '1' } },
      expectedHash: linuxHash,
    }))
    assert.equal(res.ok, true)
    assert.equal(res.hash, contentHash({ env: { X: '1' } }))
  })
})

test('updateTemplateCAS: concurrent updates to different platforms do not overwrite each other', async () => {
  await withTempAcsHome(async () => {
    const def = defaultTemplates()
    saveTemplates(def)

    const darwinHash = contentHash(def.darwin)
    const win32Hash = contentHash(def.win32)

    // Two updates for different platforms run sequentially under their own locks
    await withLock(() => {
      updateTemplateCAS('darwin', {
        settings: { env: { PLATFORM: 'darwin-updated' } },
        expectedHash: darwinHash,
      })
    })

    await withLock(() => {
      updateTemplateCAS('win32', {
        settings: { env: { PLATFORM: 'win32-updated' } },
        expectedHash: win32Hash,
      })
    })

    const reloaded = loadTemplates()
    assert.equal(reloaded.darwin.env.PLATFORM, 'darwin-updated')
    assert.equal(reloaded.win32.env.PLATFORM, 'win32-updated')
    assert.deepEqual(reloaded.linux, { env: {} })
  })
})

test('createProfileCore: source mutual exclusivity', async () => {
  await withTempAcsHome(async () => {
    const r1 = createProfileCore('test1', { fromCurrent: true, platform: 'linux' })
    assert.equal(r1.ok, false)
    assert.equal(r1.error, 'source-conflict')

    const r2 = createProfileCore('test2', { fromCurrent: true, settings: { env: {} } })
    assert.equal(r2.ok, false)
    assert.equal(r2.error, 'source-conflict')

    const r3 = createProfileCore('test3', { platform: 'linux', settings: { env: {} } })
    assert.equal(r3.ok, false)
    assert.equal(r3.error, 'source-conflict')
  })
})

test('createProfileCore: copies snapshot from platform template and changes to template do not affect profile', async () => {
  await withTempAcsHome(async () => {
    const tmpls = defaultTemplates()
    tmpls.linux = { env: { OS: 'linux-orig' }, model: 'claude-test' }
    saveTemplates(tmpls)

    const r = createProfileCore('linux-p', { platform: 'linux' })
    assert.equal(r.ok, true)
    assert.equal(r.platform, 'linux')
    assert.deepEqual(r.settings, { env: { OS: 'linux-orig' }, model: 'claude-test' })

    // Update template afterwards
    const linuxHash = contentHash(tmpls.linux)
    await withLock(() => {
      updateTemplateCAS('linux', {
        settings: { env: { OS: 'linux-new' } },
        expectedHash: linuxHash,
      })
    })

    // Profile created before remains unchanged
    const p1Settings = JSON.parse(fs.readFileSync(path.join(process.env.AGENTSWAP_HOME, 'profiles', 'linux-p.json'), 'utf8'))
    assert.equal(p1Settings.env.OS, 'linux-orig')

    // Newly created profile picks up the updated template
    const rNew = createProfileCore('linux-p2', { platform: 'linux' })
    assert.equal(rNew.settings.env.OS, 'linux-new')
  })
})
