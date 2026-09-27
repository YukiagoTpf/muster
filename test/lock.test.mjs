import { describe, test, before, after } from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { spawnSync } from 'node:child_process'
import { withLock, restoreClaimedLock, stealClaimStillValid, writeLockIfAbsent, writeAtomic, releaseLock, claimMatchesHolder } from '../src/store.mjs'

/**
 * Lock semantics. The module-level counter reentrancy was replaced with
 * AsyncLocalStorage: nested calls in the SAME async context share the hold,
 * while two CONCURRENT same-process transactions must serialize like separate
 * processes.
 */
describe('cross-process lock', () => {
  let tmp, appHome

  before(() => {
    tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'agentswap-lock-'))
    appHome = path.join(tmp, 'agentswap-home')
    process.env.AGENTSWAP_HOME = appHome
  })

  after(() => {
    fs.rmSync(tmp, { recursive: true, force: true })
  })

  test('nested withLock in the same context shares the hold (no deadlock)', async () => {
    let innerRan = false
    await withLock(async () => {
      await withLock(async () => {
        await new Promise((r) => setTimeout(r, 10))
        innerRan = true
      })
    })
    assert.ok(innerRan)
  })

  test('concurrent same-process transactions never overlap', async () => {
    let active = 0
    let maxActive = 0
    const tx = (delay) => withLock(async () => {
      active++
      maxActive = Math.max(maxActive, active)
      await new Promise((r) => setTimeout(r, delay))
      active--
    })
    await Promise.all([tx(30), tx(30), tx(30)])
    assert.equal(maxActive, 1, 'two independent transactions entered the critical section together')
  })

  test('the lock file is removed after release', async () => {
    await withLock(() => {})
    assert.ok(!fs.existsSync(path.join(appHome, 'lock')))
  })

  test('releaseLock never removes a lock that no longer names us', async () => {
    // The claim-verify-restore release must not delete a lock a contender
    // cycled onto the path: a foreign pid/token at the path is left untouched.
    const file = path.join(appHome, 'lock')
    fs.writeFileSync(file, `${process.pid}:aaaaaaaaaaaaaaaa`)
    await releaseLock(file, 'aaaaaaaaaaaaaaaa')
    assert.ok(!fs.existsSync(file), 'our own lock is released')
    fs.writeFileSync(file, '99999:bbbbbbbbbbbbbbbb')
    await releaseLock(file, 'aaaaaaaaaaaaaaaa')
    assert.equal(fs.readFileSync(file, 'utf8'), '99999:bbbbbbbbbbbbbbbb', 'a foreign lock must survive')
    fs.rmSync(file, { force: true })
  })

  test('a lock held by a dead pid is stolen', async () => {
    const dead = spawnSync(process.execPath, ['-e', '0']).pid
    fs.mkdirSync(appHome, { recursive: true })
    fs.writeFileSync(path.join(appHome, 'lock'), `${dead}:deadbeefdeadbeef`)
    let ran = false
    await withLock(() => { ran = true })
    assert.ok(ran)
  })

  test('an unparseable lock file fails closed with ELOCKFORMAT (never stolen)', async () => {
    // A lock we can't parse belongs to a different agentswap generation: a
    // contender must leave it for its owner, not corrupt-steal it.
    fs.writeFileSync(path.join(appHome, 'lock'), 'garbage-not-a-lock')
    await assert.rejects(
      () => withLock(() => {}, { timeoutMs: 200, pollMs: 30 }),
      (e) => e.code === 'ELOCKFORMAT',
    )
    assert.equal(fs.readFileSync(path.join(appHome, 'lock'), 'utf8'), 'garbage-not-a-lock', 'the foreign lock must survive untouched')
    fs.rmSync(path.join(appHome, 'lock'), { force: true })
  })

  test('a live owner is waited on, then acquired after release', async () => {
    // A real holder context (its token is in the live set): the contender
    // must WAIT, not steal.
    const held = withLock(async () => {
      await new Promise((r) => setTimeout(r, 150))
    })
    await new Promise((r) => setTimeout(r, 30)) // let the holder acquire
    let ran = false
    const contender = withLock(() => { ran = true }, { timeoutMs: 5000, pollMs: 30 })
    await Promise.all([held, contender])
    assert.ok(ran)
  })

  test('a live owner that never releases times out with ELOCKED', async () => {
    let release
    const held = new Promise((res) => { release = res })
    const holder = withLock(() => held, { timeoutMs: 5000, pollMs: 20 })
    await new Promise((r) => setTimeout(r, 30)) // let the holder acquire
    await assert.rejects(
      () => withLock(() => {}, { timeoutMs: 150, pollMs: 30 }),
      (e) => e.code === 'ELOCKED',
    )
    release()
    await holder
  })

  test('an own-pid orphan lock (dead token) is reclaimed, not waited on', async () => {
    // Simulate a failed release: the lock names our pid with a token no live
    // context holds. The next withLock must reclaim it instead of treating
    // the always-alive own pid as a live owner and timing out.
    fs.writeFileSync(path.join(appHome, 'lock'), `${process.pid}:deadbeefdeadbeef`)
    let ran = false
    await withLock(() => { ran = true })
    assert.ok(ran)
    assert.ok(!fs.existsSync(path.join(appHome, 'lock')))
  })

  test('new locks are written as pid:token', async () => {
    const file = path.join(appHome, 'lock')
    let content
    await withLock(() => { content = fs.readFileSync(file, 'utf8').trim() })
    const m = /^(\d+):([0-9a-f]+)$/.exec(content)
    assert.ok(m, `expected pid:token, got ${JSON.stringify(content)}`)
    assert.equal(Number(m[1]), process.pid)
  })

  test('withLock fences a displaced lock with ELOCKDISPLACED', async () => {
    // Simulate the irreducible userspace window: while the holder's fn runs,
    // a thief moved its live lock and a contender took the empty path. The
    // holder's transaction must fail loudly, not report success.
    let enter
    const gate = new Promise((res) => { enter = res })
    const file = path.join(appHome, 'lock')
    const held = withLock(async () => { await gate })
    await new Promise((r) => setTimeout(r, 30)) // let the holder acquire
    fs.rmSync(file, { force: true })
    fs.writeFileSync(file, '99999:0123456789abcdef')
    try {
      enter()
      await assert.rejects(held, (e) => e.code === 'ELOCKDISPLACED')
    } finally {
      fs.rmSync(file, { force: true })
    }
  })

  test('writeAtomic fences a displaced lock at the next write (ELOCKDISPLACED)', async () => {
    // Write-time fencing: a displaced holder stops writing at the next write
    // boundary, so it cannot overwrite the new holder's writes after the new
    // holder reports success.
    let enter
    const gate = new Promise((res) => { enter = res })
    const file = path.join(appHome, 'lock')
    const target = path.join(appHome, 'fenced.tmp')
    const held = withLock(async () => {
      await gate
      writeAtomic(target, 'data') // the lock was displaced while we waited
    })
    await new Promise((r) => setTimeout(r, 30)) // let the holder acquire
    fs.rmSync(file, { force: true })
    fs.writeFileSync(file, '99999:0123456789abcdef')
    try {
      enter()
      await assert.rejects(held, (e) => e.code === 'ELOCKDISPLACED')
      assert.ok(!fs.existsSync(target), 'the displaced write must not land')
    } finally {
      fs.rmSync(file, { force: true })
      fs.rmSync(target, { force: true })
    }
  })

  test('restoring a claimed lock never clobbers a fresh one (ABA guard)', () => {
    const file = path.join(appHome, 'lock')
    // The path is empty — the claimed (but live) lock is restored.
    const grave1 = path.join(appHome, 'lock.dead.test.aaaaaa')
    fs.writeFileSync(grave1, '12345:beefbeefbeefbeef')
    assert.equal(restoreClaimedLock(file, grave1), true)
    assert.equal(fs.readFileSync(file, 'utf8'), '12345:beefbeefbeefbeef')
    assert.ok(!fs.existsSync(grave1))
    fs.rmSync(file, { force: true })
    // A third contender acquired the empty path while the thief verified —
    // the restore must NOT clobber their lock (a rename restore would have).
    const grave2 = path.join(appHome, 'lock.dead.test.bbbbbb')
    fs.writeFileSync(grave2, '12345:beefbeefbeefbeef')
    fs.writeFileSync(file, '67890:cafecafecafecafe')
    assert.equal(restoreClaimedLock(file, grave2), false)
    assert.equal(fs.readFileSync(file, 'utf8'), '67890:cafecafecafecafe')
    assert.ok(!fs.existsSync(grave2))
    fs.rmSync(file, { force: true })
  })

  test('stealClaimStillValid: the re-read must match the first read and still be stale', () => {
    const dead = spawnSync(process.execPath, ['-e', '0']).pid
    const A = { pid: dead, token: 'deadbeefdeadbeef' }
    const B = { pid: dead, token: 'cafecafecafecafe' } // same dead pid, cycled token
    const live = { pid: process.pid, token: 'aaaaaaaabbbbbbbb' }
    const alive = process.ppid // a live pid that is not ours
    const aliveFresh = { pid: alive, token: 'bbbbbbbbbbbbbbbb' }
    assert.equal(stealClaimStillValid(null, null), true) // absent → still absent: steal
    assert.equal(stealClaimStillValid(null, A), false) // absent → became valid: back off
    assert.equal(stealClaimStillValid(A, A), true) // dead A → still dead A: steal
    assert.equal(stealClaimStillValid(A, B), false) // dead A → cycled B: back off
    assert.equal(stealClaimStillValid(A, live), false) // dead A → live holder: back off
    assert.equal(stealClaimStillValid(live, live), false) // own pid, default predicate: never steal
    assert.equal(stealClaimStillValid(live, live, () => true), false) // own pid + live token: wait
    assert.equal(stealClaimStillValid(live, live, () => false), true) // own pid + dead token: orphan, steal
    assert.equal(stealClaimStillValid(aliveFresh, aliveFresh), false) // alive pid: wait
  })

  test('claimMatchesHolder: pid and token must match', () => {
    const dead = spawnSync(process.execPath, ['-e', '0']).pid
    const stale = { pid: dead, token: 'deadbeefdeadbeef' }
    const same = { pid: dead, token: 'deadbeefdeadbeef' }
    const cycled = { pid: dead, token: 'cafecafecafecafe' }
    assert.equal(claimMatchesHolder(same, stale), true) // unchanged: the dead lock is ours to remove
    assert.equal(claimMatchesHolder(cycled, stale), false) // token cycled: live lock, restore
    assert.equal(claimMatchesHolder(null, stale), false) // grave unreadable: do not delete
  })

  test('writeLockIfAbsent writes only when the path is empty (never clobbers)', () => {
    // The restore fallback writes the displaced owner's own lock content back
    // at the path — it must never overwrite a contender's fresh lock.
    const file = path.join(appHome, 'lock')
    assert.equal(writeLockIfAbsent(file, '111:aaaaaaaaaaaaaaaa'), true)
    assert.equal(fs.readFileSync(file, 'utf8'), '111:aaaaaaaaaaaaaaaa')
    assert.equal(writeLockIfAbsent(file, '222:bbbbbbbbbbbbbbbb'), false)
    assert.equal(fs.readFileSync(file, 'utf8'), '111:aaaaaaaaaaaaaaaa', 'must not clobber an existing lock')
    fs.rmSync(file, { force: true })
  })

  test('a persistent restore failure leaves the grave and throws, not an empty path claim', {
    skip: process.platform === 'win32'
      ? 'win32 has no POSIX directory modes: chmod 0500 does not make the dir read-only'
      : typeof process.getuid === 'function' && process.getuid() === 0
        ? 'root bypasses directory permissions (chmod 0500 is writable)' : false,
  }, () => {
    // When link(2) fails persistently (here: the dir is read-only), the
    // write-back fallback fails the same way: the grave must survive as
    // evidence and the error must surface — the path is NOT claimed protected.
    const file = path.join(appHome, 'lock')
    const grave = path.join(appHome, 'lock.dead.test.eeeeee')
    fs.writeFileSync(grave, '12345:beefbeefbeefbeef')
    fs.chmodSync(appHome, 0o500)
    try {
      assert.throws(() => restoreClaimedLock(file, grave))
      assert.ok(fs.existsSync(grave), 'grave must be preserved on persistent failure')
    } finally {
      fs.chmodSync(appHome, 0o700)
    }
    fs.rmSync(grave, { force: true })
  })
})
