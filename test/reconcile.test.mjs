import { test } from 'node:test'
import assert from 'node:assert/strict'
import { reconcile } from '../src/reconcile.mjs'

const H = { liveHash: 'L', profileHash: 'P', baselineHash: 'B' }

test('no baseline: equal → apply; different → needsChoice', () => {
  const r1 = reconcile({ liveHash: 'X', profileHash: 'X', baselineHash: null })
  assert.equal(r1.state, 'no-baseline')
  assert.equal(r1.needsChoice, false)
  assert.equal(r1.canApply, true)

  const r2 = reconcile({ liveHash: 'X', profileHash: 'Y', baselineHash: null })
  assert.equal(r2.needsChoice, true)
  assert.equal(r2.canApply, false)
})

test('in-sync when nothing changed', () => {
  const r = reconcile({ liveHash: 'B', profileHash: 'B', baselineHash: 'B' })
  assert.equal(r.state, 'in-sync')
})

test('live-unsaved: only live changed → canSave', () => {
  const r = reconcile({ liveHash: 'L2', profileHash: 'B', baselineHash: 'B' })
  assert.equal(r.state, 'live-unsaved')
  assert.equal(r.canSave, true)
})

test('profile-edited: only profile changed → canApply, never save', () => {
  const r = reconcile({ liveHash: 'B', profileHash: 'P2', baselineHash: 'B' })
  assert.equal(r.state, 'profile-edited')
  assert.equal(r.canApply, true)
  assert.equal(r.canSave, false)
})

test('both changed: converged if equal, else conflict', () => {
  const c = reconcile({ liveHash: 'X', profileHash: 'X', baselineHash: 'B' })
  assert.equal(c.state, 'converged')
  const f = reconcile({ liveHash: 'X', profileHash: 'Y', baselineHash: 'B' })
  assert.equal(f.state, 'conflict')
  assert.equal(f.needsChoice, true)
})
