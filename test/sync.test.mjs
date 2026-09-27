import { test } from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import {
  syncReconcile, readSyncState, writeSyncState,
  syncBaseline, recordSyncBaseline, forgetRemote,
} from '../src/sync.mjs'

test('syncReconcile: direction table', () => {
  const A = 'hash-a', B = 'hash-b', C = 'hash-c'
  const cases = [
    [{ localHash: null, remoteHash: null, baselineHash: null }, 'absent'],
    [{ localHash: A, remoteHash: null, baselineHash: null }, 'push'],
    [{ localHash: null, remoteHash: B, baselineHash: null }, 'pull'],
    [{ localHash: A, remoteHash: A, baselineHash: null }, 'in-sync'],
    [{ localHash: A, remoteHash: A, baselineHash: C }, 'in-sync'],
    [{ localHash: A, remoteHash: B, baselineHash: A }, 'pull'],
    [{ localHash: A, remoteHash: B, baselineHash: B }, 'push'],
    [{ localHash: A, remoteHash: B, baselineHash: C }, 'conflict'],
    [{ localHash: A, remoteHash: B, baselineHash: null }, 'conflict'],
    // deletions do not propagate: the surviving side re-adopts
    [{ localHash: null, remoteHash: B, baselineHash: B }, 'pull'],
    [{ localHash: A, remoteHash: null, baselineHash: A }, 'push'],
  ]
  for (const [input, expected] of cases) {
    assert.equal(syncReconcile(input).state, expected, JSON.stringify(input))
  }
})

test('sync state: roundtrip, baseline helpers, fail-closed on garbage', () => {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'agentswap-sync-'))
  process.env.AGENTSWAP_HOME = tmp
  try {
    assert.deepEqual(readSyncState(), { version: 1, remotes: {} })

    const state = readSyncState()
    recordSyncBaseline(state, 'vps', 'kimi', 'aaa')
    recordSyncBaseline(state, 'vps', 'sonnet', 'bbb')
    recordSyncBaseline(state, 'devbox', 'kimi', 'ccc')
    writeSyncState(state)

    const reread = readSyncState()
    assert.equal(syncBaseline(reread, 'vps', 'kimi'), 'aaa')
    assert.equal(syncBaseline(reread, 'vps', 'sonnet'), 'bbb')
    assert.equal(syncBaseline(reread, 'devbox', 'kimi'), 'ccc')
    assert.equal(syncBaseline(reread, 'vps', 'missing'), null)
    assert.ok(reread.remotes.vps.profiles.kimi.ts)

    forgetRemote(reread, 'vps')
    writeSyncState(reread)
    assert.deepEqual(Object.keys(readSyncState().remotes), ['devbox'])

    // corrupt file → empty state (no baseline = fail-closed), never throws
    fs.writeFileSync(path.join(tmp, 'sync.json'), '{ not json')
    assert.deepEqual(readSyncState(), { version: 1, remotes: {} })
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true })
  }
})
