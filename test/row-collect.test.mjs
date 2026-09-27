import { describe, test } from 'node:test'
import assert from 'node:assert/strict'
import { collectList, collectPlugins, collectMarkets } from '../src/web/public/row-collect.js'

// Hydration snapshot the DOM keeps per editor: did the file have the key, its
// raw value, and what the rows showed right after loading.
const snap = (had, raw, hyd) => ({ had, raw, hyd })

describe('list rows', () => {
  const raw = ['Bash(ls)', ' spaced ', '']
  const hydrated = raw.map((v) => ({ value: v, hyd: v }))

  test('untouched rows write the original back verbatim', () => {
    assert.deepEqual(collectList(hydrated, snap(true, raw, raw)), { value: raw })
    assert.deepEqual(collectList([], snap(false, undefined, [])), { absent: true })
  })

  test('edited rows are trimmed; untouched rows keep their exact text; blank new rows are dropped', () => {
    const rows = [{ value: 'Bash(ls)', hyd: 'Bash(ls)' }, { value: ' spaced ', hyd: ' spaced ' }, { value: '  new  ' }, { value: '   ' }]
    assert.deepEqual(collectList(rows, snap(true, raw, raw)), { value: ['Bash(ls)', ' spaced ', 'new'] })
  })

  test('removing every row writes an empty list, never the original back', () => {
    assert.deepEqual(collectList([], snap(true, ['Bash(ls)'], ['Bash(ls)'])), { value: [] })
    assert.deepEqual(collectList([{ value: '' }], snap(true, ['Bash(ls)'], ['Bash(ls)'])), { value: [] })
  })

  test('rows added and removed again on an absent key leave it absent', () => {
    assert.deepEqual(collectList([{ value: '  ' }], snap(false, undefined, [])), { absent: true })
  })
})

describe('plugin rows', () => {
  const raw = { 'a@b': true, 'c@d': false }
  const hyd = Object.entries(raw)
  const hydrated = hyd.map(([k, on]) => ({ name: k, hyd: k, on }))

  test('untouched rows write the original back verbatim', () => {
    assert.deepEqual(collectPlugins(hydrated, snap(true, raw, hyd)), { value: raw })
  })

  test('removing every row writes an empty map, never the original back', () => {
    const r = collectPlugins([], snap(true, raw, hyd))
    assert.deepEqual({ ...r.value }, {})
  })

  test('removing every row of an absent key leaves it absent', () => {
    assert.deepEqual(collectPlugins([{ name: '', hyd: '', on: true }], snap(false, undefined, [])), { absent: true })
  })

  test('edits toggle and rename; a key spelled __proto__ stays a real own key', () => {
    const rows = [{ name: 'a@b', hyd: 'a@b', on: false }, { name: ' __proto__ ', hyd: '', on: true }]
    const { value } = collectPlugins(rows, snap(true, raw, hyd))
    assert.deepEqual(Object.keys(value), ['a@b', '__proto__'])
    assert.equal(value['a@b'], false)
    assert.equal(Object.getOwnPropertyDescriptor(value, '__proto__').value, true)
  })
})

describe('marketplace rows', () => {
  const raw = { mk: { source: { source: 'github', repo: 'o/r' }, autoUpdate: false } }
  const hyd = [{ name: 'mk', type: 'github', value: 'o/r', autoUpdate: false, hadAuto: true }]
  const row = (over = {}) => ({ name: 'mk', nameHyd: 'mk', type: 'github', field: 'repo', value: 'o/r', valueHyd: 'o/r', autoUpdate: false, hadAuto: true, ...over })

  test('untouched rows write the original back verbatim', () => {
    assert.deepEqual(collectMarkets([row()], snap(true, raw, hyd)), { value: raw })
  })

  test('removing every row writes an empty map, never the original back', () => {
    const r = collectMarkets([], snap(true, raw, hyd))
    assert.deepEqual({ ...r.value }, {})
  })

  test('an edited row is rebuilt; autoUpdate is kept only when set or already present', () => {
    const { value } = collectMarkets([row({ value: ' o/x ' }), row({ name: 'new', nameHyd: '', value: 'p/q', valueHyd: '', hadAuto: false })], snap(true, raw, hyd))
    assert.deepEqual({ ...value }, {
      mk: { source: { source: 'github', repo: 'o/x' }, autoUpdate: false },
      new: { source: { source: 'github', repo: 'p/q' } },
    })
  })
})
