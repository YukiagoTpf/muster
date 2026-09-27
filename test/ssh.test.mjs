import { describe, test, before, after } from 'node:test'
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { installFakeSsh } from './helpers/fake-ssh.mjs'
import {
  shellQuote, buildRemoteCommand, sshRun, runRemoteJson, probeRemote,
} from '../src/ssh.mjs'

const skipSsh = process.platform === 'win32'

describe('ssh transport', () => {
  test('shellQuote wraps in single quotes and escapes embedded quotes', () => {
    assert.equal(shellQuote('abc'), "'abc'")
    assert.equal(shellQuote(''), "''")
    assert.equal(shellQuote("a'b"), "'a'\\''b'")
    assert.equal(shellQuote("it's"), "'it'\\''s'")
  })

  test('buildRemoteCommand quotes bin/name and always ends with --json', () => {
    assert.equal(buildRemoteCommand('agentswap', 'list'), `'agentswap' __sync list --json`)
    assert.equal(buildRemoteCommand('agentswap', 'read', 'work'), `'agentswap' __sync read 'work' --json`)
    assert.equal(buildRemoteCommand('/opt/as', 'write', 'a-b'), `'/opt/as' __sync write 'a-b' --json`)
  })

  test('shellQuote round-trips through sh', { skip: skipSsh }, () => {
    const samples = ['plain', "it's", 'a "quote"', '$HOME', '`ticks`', 'a b', '']
    for (const s of samples) {
      const out = execFileSync('sh', ['-c', `printf %s ${shellQuote(s)}`], { encoding: 'utf8' })
      assert.equal(out, s, JSON.stringify(s))
    }
  })
})

describe('sshRun / runRemoteJson / probeRemote', () => {
  let tmp, sshDir, fakeBin, origPath

  before(() => {
    tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'agentswap-ssh-'))
    const remoteHome = path.join(tmp, 'remote-home')
    fs.mkdirSync(remoteHome, { recursive: true })
    sshDir = path.join(tmp, 'bin')
    installFakeSsh(sshDir, { testhost: remoteHome })
    fakeBin = installFakeBin(path.join(tmp, 'fakebin'))
    origPath = process.env.PATH
    process.env.PATH = `${sshDir}${path.delimiter}${origPath}`
  })

  after(() => {
    process.env.PATH = origPath
    fs.rmSync(tmp, { recursive: true, force: true })
  })

  test('sshRun captures stdout', { skip: skipSsh }, async () => {
    const r = await sshRun('testhost', 'printf %s hello')
    assert.equal(r.stdout, 'hello')
  })

  test('sshRun passes stdin through', { skip: skipSsh }, async () => {
    const r = await sshRun('testhost', 'cat', { input: 'piped-bytes' })
    assert.equal(r.stdout, 'piped-bytes')
  })

  test('sshRun rejects non-zero exit with ESSH', { skip: skipSsh }, async () => {
    await assert.rejects(
      () => sshRun('testhost', 'exit 7'),
      (e) => e.code === 'ESSH' && /ssh exited 7/.test(e.message),
    )
  })

  test('sshRun rejects unknown host with ESSH', { skip: skipSsh }, async () => {
    await assert.rejects(
      () => sshRun('no-such-host', 'true'),
      (e) => e.code === 'ESSH' && /unknown host: no-such-host/.test(e.message),
    )
  })

  test('sshRun rejects with ESSH (no crash) when ssh exits before reading a large request', { skip: skipSsh }, async () => {
    // The fake ssh exits at once for an unknown host without touching stdin;
    // a body larger than the pipe buffer then hits EPIPE on our side.
    await assert.rejects(
      () => sshRun('no-such-host', 'cat', { input: 'x'.repeat(4 * 1024 * 1024) }),
      (e) => e.code === 'ESSH' && /unknown host: no-such-host/.test(e.message),
    )
  })

  test('runRemoteJson parses a successful envelope', { skip: skipSsh }, async () => {
    const parsed = await runRemoteJson({ host: 'testhost', bin: fakeBin }, 'list')
    assert.equal(parsed.version, 2)
    assert.equal(parsed.profiles.work, 'deadbeef')
  })

  test('runRemoteJson wraps a remote error envelope as EREMOTE', { skip: skipSsh }, async () => {
    process.env.FAKE_BIN_MODE = 'error'
    try {
      await assert.rejects(
        () => runRemoteJson({ host: 'testhost', bin: fakeBin }, 'read', 'x'),
        (e) => e.code === 'EREMOTE' && e.remoteError === 'not-found' && /profile not found: x/.test(e.message),
      )
    } finally {
      delete process.env.FAKE_BIN_MODE
    }
  })

  test('runRemoteJson throws EREMOTEBADJSON on garbage output', { skip: skipSsh }, async () => {
    process.env.FAKE_BIN_MODE = 'garbage'
    try {
      await assert.rejects(
        () => runRemoteJson({ host: 'testhost', bin: fakeBin }, 'list'),
        (e) => e.code === 'EREMOTEBADJSON' && /non-JSON output/.test(e.message),
      )
    } finally {
      delete process.env.FAKE_BIN_MODE
    }
  })

  test('probeRemote accepts a remote that speaks __sync', { skip: skipSsh }, async () => {
    await probeRemote({ host: 'testhost', bin: fakeBin })
  })

  test('probeRemote rejects a remote that does not speak __sync', { skip: skipSsh }, async () => {
    process.env.FAKE_BIN_MODE = 'garbage'
    try {
      await assert.rejects(
        () => probeRemote({ host: 'testhost', bin: fakeBin }),
        (e) => e.code === 'EREMOTEBADJSON',
      )
    } finally {
      delete process.env.FAKE_BIN_MODE
    }
  })
})

/** Standalone fake remote `bin` driven by FAKE_BIN_MODE. */
function installFakeBin(dir) {
  fs.mkdirSync(dir, { recursive: true })
  const dest = path.join(dir, 'fake-agentswap')
  const script = [
    '#!/usr/bin/env node',
    "'use strict'",
    'const args = process.argv.slice(2)',
    'const mode = process.env.FAKE_BIN_MODE || "ok"',
    'if (args[0] === "--version" || args[0] === "version") {',
    '  if (mode === "bad-version") { process.stdout.write("not-a-version\\n"); process.exit(0) }',
    '  process.stdout.write("1.2.3\\n")',
    '  process.exit(0)',
    '}',
    'if (mode === "garbage") { process.stdout.write("%%%not-json%%%\\n"); process.exit(0) }',
    'if (mode === "error") {',
    '  process.stdout.write(JSON.stringify({ version: 2, error: "not-found", detail: "profile not found: x" }) + "\\n")',
    '  process.exit(1)',
    '}',
    'process.stdout.write(JSON.stringify({ version: 2, profiles: { work: "deadbeef" }, errors: [] }) + "\\n")',
    'process.exit(0)',
    '',
  ].join('\n')
  fs.writeFileSync(dest, script)
  fs.chmodSync(dest, 0o755)
  return dest
}
