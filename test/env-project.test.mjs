import { describe, test } from 'node:test'
import assert from 'node:assert/strict'
import {
  applyEnvEdit, projectEnv, PLATFORMS, resolveMeta, parseLaunchArgsText,
} from '../src/web/public/env-project.js'

describe('env projection', () => {
  test('platforms are the three Node process.platform values', () => {
    assert.deepEqual(PLATFORMS, ['darwin', 'linux', 'win32'])
  })

  test('projects string rows and locks null / non-string values', () => {
    const p = projectEnv({
      model: 'keep-me',
      unknown: { nested: true },
      env: {
        A: 'one',
        B: '',
        N: null,
        O: { x: 1 },
        Arr: ['x'],
      },
    })
    assert.equal(p.kind, 'ok')
    assert.deepEqual(p.rows, [
      { key: 'A', value: 'one', locked: false },
      { key: 'B', value: '', locked: false },
      { key: 'N', value: null, locked: true, reason: 'null' },
      { key: 'O', value: { x: 1 }, locked: true, reason: 'object' },
      { key: 'Arr', value: ['x'], locked: true, reason: 'array' },
    ])
  })

  test('missing env is distinct from an empty object env', () => {
    assert.equal(projectEnv({ model: 'm' }).kind, 'missing')
    assert.equal(projectEnv({ env: {} }).kind, 'ok')
    assert.deepEqual(projectEnv({ env: {} }).rows, [])
  })

  test('non-object env locks the whole table', () => {
    assert.equal(projectEnv({ env: null }).kind, 'locked')
    assert.equal(projectEnv({ env: 'x' }).kind, 'locked')
    assert.equal(projectEnv({ env: [] }).kind, 'locked')
    assert.equal(projectEnv(null).kind, 'locked')
  })

  test('apply keeps non-env keys and does not invent model', () => {
    const src = { model: 'sonnet', extra: [1], env: { A: '1' } }
    const r = applyEnvEdit(src, { entries: [{ key: 'A', value: '2' }, { key: 'B', value: '' }] })
    assert.equal(r.ok, true)
    assert.equal(r.settings.model, 'sonnet')
    assert.deepEqual(r.settings.extra, [1])
    assert.equal(JSON.stringify(r.settings.env), JSON.stringify({ A: '2', B: '' }))
    assert.equal(Object.getPrototypeOf(r.settings.env), null)
    // source not mutated
    assert.deepEqual(src.env, { A: '1' })
  })

  test('delete removes a key; empty string keeps it', () => {
    const src = { env: { KEEP: '', DROP: 'x', STAY: 'y' } }
    const deleted = applyEnvEdit(src, {
      entries: [{ key: 'KEEP', value: '' }, { key: 'STAY', value: 'y' }],
    })
    assert.equal(deleted.ok, true)
    assert.equal(JSON.stringify(deleted.settings.env), JSON.stringify({ KEEP: '', STAY: 'y' }))
    assert.equal(Object.prototype.hasOwnProperty.call(deleted.settings.env, 'DROP'), false)
  })

  test('null and non-string env values survive untouched', () => {
    const src = {
      env: { S: 'a', N: null, O: { nested: true } },
      hooks: { Stop: [] },
    }
    const r = applyEnvEdit(src, { entries: [{ key: 'S', value: 'b' }] })
    assert.equal(r.ok, true)
    assert.equal(r.settings.env.N, null)
    assert.deepEqual(r.settings.env.O, { nested: true })
    assert.deepEqual(r.settings.hooks, { Stop: [] })
  })

  test('refuses to overwrite or collide with a locked env key', () => {
    const src = { env: { N: null, S: 'ok' } }
    assert.equal(applyEnvEdit(src, { entries: [{ key: 'N', value: 'nope' }] }).reason, 'locked-key')
    assert.equal(applyEnvEdit(src, {
      entries: [{ key: 'S', value: '1' }, { key: 'S', value: '2' }],
    }).reason, 'duplicate-key')
  })

  test('refuses when env itself is a locked shape', () => {
    assert.equal(applyEnvEdit({ env: null }, { entries: [] }).reason, 'env-locked')
  })

  test('does not invent env when settings had none and the table is empty', () => {
    const r = applyEnvEdit({ model: 'm' }, { entries: [] })
    assert.equal(r.ok, true)
    assert.equal(Object.prototype.hasOwnProperty.call(r.settings, 'env'), false)
    assert.equal(r.settings.model, 'm')
  })

  test('creates env when adding the first string row to a settings without env', () => {
    const r = applyEnvEdit({ model: 'm' }, { entries: [{ key: 'K', value: 'v' }] })
    assert.equal(r.ok, true)
    assert.equal(JSON.stringify(r.settings.env), JSON.stringify({ K: 'v' }))
    assert.equal(Object.getPrototypeOf(r.settings.env), null)
    assert.equal(r.settings.model, 'm')
  })

  test('unedited launchArgs keeps multi-word entries that space-split would break', () => {
    const orig = {
      notes: 'n',
      launchArgs: ['--append-system-prompt', 'two words'],
      unsetEnv: ['FOO'],
    }
    const kept = resolveMeta(orig, {}, {
      notes: 'changed',
      launchArgsText: '--append-system-prompt two words',
      unsetEnvText: 'FOO, BAR',
    })
    assert.deepEqual(kept.launchArgs, ['--append-system-prompt', 'two words'])
    assert.equal(kept.notes, 'n')
    assert.deepEqual(kept.unsetEnv, ['FOO'])

    const edited = resolveMeta(orig, { launchArgs: true, notes: true }, {
      notes: 'changed',
      launchArgsText: '--append-system-prompt two words',
      unsetEnvText: 'FOO',
    })
    assert.equal(edited.notes, 'changed')
    assert.deepEqual(edited.launchArgs, parseLaunchArgsText('--append-system-prompt two words'))
    assert.deepEqual(edited.launchArgs, ['--append-system-prompt', 'two', 'words'])
  })

  test('unedited null launchArgs stays null (global default), not []', () => {
    const kept = resolveMeta(
      { notes: '', launchArgs: null, unsetEnv: [] },
      {},
      { notes: '', launchArgsText: '', unsetEnvText: '' },
    )
    assert.equal(kept.launchArgs, null)
  })

  test('__proto__ and constructor stay own env keys (JSON.parse shape)', () => {
    // Same construction settings.mjs / settings.test.mjs cover: a real
    // __proto__ key must not become a prototype setter, and must round-trip.
    const src = JSON.parse('{"model":"m","env":{"__proto__":{"x":1},"constructor":"c","A":"a"}}')
    const projected = projectEnv(src)
    assert.equal(projected.kind, 'ok')
    assert.ok(projected.rows.some((r) => r.key === '__proto__' && r.locked))
    assert.ok(projected.rows.some((r) => r.key === 'constructor' && !r.locked))

    const r = applyEnvEdit(src, {
      entries: [
        { key: 'A', value: 'b' },
        { key: 'constructor', value: 'c' },
        { key: '__proto__', value: 'as-string' },
      ],
    })
    // locked object __proto__ blocks a string overwrite of the same key
    assert.equal(r.ok, false)
    assert.equal(r.reason, 'locked-key')
    assert.equal(r.key, '__proto__')

    const keepLocked = applyEnvEdit(src, {
      entries: [
        { key: 'A', value: 'b' },
        { key: 'constructor', value: 'c2' },
      ],
    })
    assert.equal(keepLocked.ok, true)
    const env = keepLocked.settings.env
    assert.equal(Object.getPrototypeOf(env), null)
    assert.ok(hasOwn(env, '__proto__'))
    assert.deepEqual(env['__proto__'], { x: 1 })
    assert.notEqual(Object.getPrototypeOf(env), env['__proto__'])
    assert.equal(env.constructor, 'c2')
    assert.equal(env.A, 'b')
    assert.equal(keepLocked.settings.model, 'm')

    const setProtoString = applyEnvEdit({ env: {} }, {
      entries: [{ key: '__proto__', value: 'x' }, { key: 'constructor', value: 'y' }],
    })
    assert.equal(setProtoString.ok, true)
    assert.ok(hasOwn(setProtoString.settings.env, '__proto__'))
    assert.equal(setProtoString.settings.env['__proto__'], 'x')
    assert.equal(Object.getPrototypeOf(setProtoString.settings.env), null)
  })

  test('preserves original env key order; locked keys stay interleaved', () => {
    const src = {
      env: { A: '1', N: null, B: '2', O: { x: 1 }, C: '3' },
    }
    const noop = applyEnvEdit(src, {
      entries: [
        { key: 'A', value: '1' },
        { key: 'B', value: '2' },
        { key: 'C', value: '3' },
      ],
    })
    assert.equal(noop.ok, true)
    assert.deepEqual(Object.keys(noop.settings.env), ['A', 'N', 'B', 'O', 'C'])

    const deleted = applyEnvEdit(src, {
      entries: [
        { key: 'A', value: '1' },
        { key: 'C', value: '3x' },
      ],
    })
    assert.equal(deleted.ok, true)
    assert.deepEqual(Object.keys(deleted.settings.env), ['A', 'N', 'O', 'C'])
    assert.equal(deleted.settings.env.C, '3x')
    assert.equal(Object.prototype.hasOwnProperty.call(deleted.settings.env, 'B'), false)

    const added = applyEnvEdit(src, {
      entries: [
        { key: 'A', value: '1' },
        { key: 'B', value: '2' },
        { key: 'C', value: '3' },
        { key: 'D', value: 'new' },
      ],
    })
    assert.equal(added.ok, true)
    assert.deepEqual(Object.keys(added.settings.env), ['A', 'N', 'B', 'O', 'C', 'D'])
  })

  test('keys with leading/trailing spaces are kept verbatim (not trimmed)', () => {
    const src = { env: { '  spaced  ': 'v', ok: '1' } }
    const r = applyEnvEdit(src, {
      entries: [
        { key: '  spaced  ', value: 'v2' },
        { key: 'ok', value: '1' },
      ],
    })
    assert.equal(r.ok, true)
    assert.equal(r.settings.env['  spaced  '], 'v2')
    assert.equal(Object.prototype.hasOwnProperty.call(r.settings.env, 'spaced'), false)
    assert.deepEqual(Object.keys(r.settings.env), ['  spaced  ', 'ok'])
  })
})

function hasOwn(obj, key) {
  return Object.prototype.hasOwnProperty.call(obj, key)
}
