import { describe, test, before, after } from 'node:test'
import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { writeJournal, journalPath } from '../src/store.mjs'
import { contentHash } from '../src/settings.mjs'

const here = path.dirname(fileURLToPath(import.meta.url))
const bin = path.join(here, '..', 'bin', 'agentswap.mjs')

describe('cli e2e', () => {
  let tmp, appHome, claudeDir, settingsPath, fakeBin
  const env = () => ({
    ...process.env,
    AGENTSWAP_HOME: appHome,
    CLAUDE_CONFIG_DIR: claudeDir,
    // A no-op editor on every platform (Windows has no `true`); the node path
    // itself often contains spaces, so it is quoted.
    EDITOR: `"${process.execPath}" -e 0`,
    VISUAL: `"${process.execPath}" -e 0`,
    PATH: `${fakeBin}${path.delimiter}${process.env.PATH}`,
  })

  function agentswap(...args) {
    const r = spawnSync(process.execPath, [bin, ...args], { env: env(), encoding: 'utf8' })
    return { code: r.status, out: r.stdout, err: r.stderr }
  }
  function agentswapJson(...args) {
    const r = agentswap(...args)
    if (r.code !== 0) throw new Error(`agentswap ${args.join(' ')} exited ${r.code}: ${r.err || r.out}`)
    return JSON.parse(r.out)
  }

  before(() => {
    tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'agentswap-cli-'))
    appHome = path.join(tmp, 'agentswap-home')
    claudeDir = path.join(tmp, 'claude')
    settingsPath = path.join(claudeDir, 'settings.json')
    fs.mkdirSync(claudeDir, { recursive: true })
    fs.writeFileSync(settingsPath, JSON.stringify({
      model: 'm1',
      env: { ANTHROPIC_BASE_URL: 'https://a.example.com', ANTHROPIC_AUTH_TOKEN: 'sk-aaaaaaaaaaaaaaaaaaaaaaaa' },
    }))
    // Fake agent CLI: doctor probes `claude --help` for run-mode flags. A real
    // agent CLI is not part of a healthy agentswap setup, so the test ships
    // its own to stay hermetic on CI runners.
    fakeBin = path.join(tmp, 'bin')
    fs.mkdirSync(fakeBin, { recursive: true })
    const fakeClaude = path.join(fakeBin, 'claude')
    fs.writeFileSync(fakeClaude, '#!/bin/sh\necho "claude --setting-sources --settings"\n')
    fs.chmodSync(fakeClaude, 0o755)
    if (process.platform === 'win32') {
      fs.writeFileSync(path.join(fakeBin, 'claude.cmd'), '@echo claude --setting-sources --settings\r\n')
    }
    // In-process helpers (writeJournal/journalPath) must target the temp home
    // AND the temp config dir — the journal filename is fingerprint-suffixed,
    // so the test process and the agentswap subprocess must compute the same
    // fingerprint from the same CLAUDE_CONFIG_DIR.
    process.env.AGENTSWAP_HOME = appHome
    process.env.CLAUDE_CONFIG_DIR = claudeDir
  })

  after(() => {
    fs.rmSync(tmp, { recursive: true, force: true })
  })

  test('add --from-current snapshots live settings; add creates a template', () => {
    let r = agentswap('add', 'work', '--from-current')
    assert.equal(r.code, 0)
    r = agentswap('add', 'personal')
    assert.equal(r.code, 0)
    const list = agentswapJson('list', '--json')
    assert.deepEqual(list.profiles.map(p => p.name).sort(), ['personal', 'work'])
  })

  test('add --platform validates platform and creates profile with chosen platform template', () => {
    // 1. Invalid platform flag
    let r = agentswap('add', 'p-invalid', '--platform', 'freebsd')
    assert.equal(r.code, 1)
    assert.match(r.out, /invalid platform "freebsd"/)

    // 2. Missing value for --platform flag
    r = agentswap('add', 'p-no-val', '--platform')
    assert.equal(r.code, 1)
    assert.match(r.out, /invalid platform ""/)

    // 3. Conflict between --from-current and --platform
    r = agentswap('add', 'p-conflict', '--from-current', '--platform', 'linux')
    assert.equal(r.code, 1)
    assert.match(r.out, /cannot combine --from-current with --platform/)

    // 4. Valid platform creates profile with template
    r = agentswap('add', 'p-linux', '--platform', 'linux')
    assert.equal(r.code, 0)
    assert.match(r.out, /created profile: p-linux \(linux template\)/)
    const pLinuxBytes = fs.readFileSync(path.join(appHome, 'profiles', 'p-linux.json'), 'utf8')
    assert.deepEqual(JSON.parse(pLinuxBytes), { env: {} })

    // 5. Valid platform in --json mode returns machine-readable envelope
    const jsonRes = agentswapJson('add', 'p-win', '--platform', 'win32', '--json')
    assert.equal(jsonRes.version, 2)
    assert.equal(jsonRes.name, 'p-win')
    assert.equal(jsonRes.platform, 'win32')
    assert.equal(jsonRes.fromCurrent, false)
  })


  test('add --platform accepts any case/padding of a known platform', () => {
    const r = agentswapJson('add', 'p-padded', '--platform', ' Linux ', '--json')
    assert.equal(r.platform, 'linux')
  })

  test('add --from-current refuses unreadable live settings with the shared error code', () => {
    const before = fs.readFileSync(settingsPath, 'utf8')
    fs.writeFileSync(settingsPath, '{not json')
    try {
      const r = agentswap('add', 'from-garbage', '--from-current', '--json')
      assert.equal(r.code, 1)
      assert.equal(JSON.parse(r.out).error, 'settings-unreadable')
      assert.ok(!fs.existsSync(path.join(appHome, 'profiles', 'from-garbage.json')))
    } finally {
      fs.writeFileSync(settingsPath, before)
    }
  })

  test('add --json errors remain machine-readable and never expose malformed template contents', () => {
    for (const args of [
      ['add', 'json-invalid', '--platform', 'freebsd'],
      ['add', 'json-missing', '--platform'],
      ['add', 'json-conflict', '--from-current', '--platform', 'linux'],
    ]) {
      const r = agentswap(...args, '--json')
      assert.equal(r.code, 1)
      const body = JSON.parse(r.out)
      assert.equal(body.version, 2)
      assert.equal(typeof body.error, 'string')
    }
    const templates = path.join(appHome, 'templates.json')
    fs.writeFileSync(templates, 'synthetic-secret-invalid-json')
    try {
      const r = agentswap('add', 'json-corrupt', '--json')
      assert.equal(r.code, 1)
      assert.equal(JSON.parse(r.out).error, 'ETEMPLATESPARSE')
      assert.ok(!r.out.includes('synthetic-secret'))
      assert.equal(fs.existsSync(path.join(appHome, 'profiles', 'json-corrupt.json')), false)
    } finally {
      fs.unlinkSync(templates)
    }
  })

  test('reserved and invalid names are rejected', () => {
    assert.equal(agentswap('add', 'use').code, 1)
    assert.equal(agentswap('add', '-bad').code, 1)
    assert.equal(agentswap('add', 'work').code, 1) // exists
  })

  test('path traversal in profile names is rejected everywhere', () => {
    assert.equal(agentswap('show', '../evil').code, 1)
    assert.equal(agentswap('show', '..').code, 1)
    assert.equal(agentswap('show', 'a/b').code, 1)
    assert.equal(agentswap('diff', '../evil', 'work').code, 1)
    assert.equal(agentswap('rm', '../evil').code, 1)
  })

  test('use switches verbatim and current reports in-sync', () => {
    const r = agentswapJson('use', 'work', '--json')
    assert.equal(r.to, 'work')
    assert.equal(r.skipped, false)
    const live = JSON.parse(fs.readFileSync(settingsPath, 'utf8'))
    assert.equal(live.model, 'm1')
    const cur = agentswapJson('current', '--json')
    assert.equal(cur.active, 'work')
    assert.equal(cur.status, 'in-sync')
  })

  test('switching away with unsaved live changes is blocked; --discard applies', () => {
    fs.writeFileSync(settingsPath, JSON.stringify({ model: 'drifted' }))
    const blocked = agentswap('use', 'personal', '--json')
    assert.equal(blocked.code, 3)
    assert.match(blocked.out, /unsaved/)
    const applied = agentswapJson('use', 'personal', '--discard', '--json')
    assert.equal(applied.to, 'personal')
    assert.equal(JSON.parse(fs.readFileSync(settingsPath, 'utf8')).model, undefined)
  })

  test('save stores live drift into the active profile', () => {
    agentswapJson('use', 'work', '--discard', '--json')
    fs.writeFileSync(settingsPath, JSON.stringify({
      model: 'm1',
      env: { ANTHROPIC_BASE_URL: 'https://a.example.com', ANTHROPIC_AUTH_TOKEN: 'sk-aaaaaaaaaaaaaaaaaaaaaaaa' },
      permissions: { allow: ['Bash(ls)'] },
    }))
    const cur = agentswapJson('current', '--json')
    assert.equal(cur.status, 'live-unsaved')
    const saved = agentswapJson('save', '--json')
    assert.equal(saved.saved, 'work')
    const prof = JSON.parse(fs.readFileSync(path.join(appHome, 'profiles', 'work.json'), 'utf8'))
    assert.deepEqual(prof.permissions, { allow: ['Bash(ls)'] })
    assert.equal(agentswapJson('current', '--json').status, 'in-sync')
  })

  test('conflict: both sides changed → save and switch refuse without flags', () => {
    fs.writeFileSync(settingsPath, JSON.stringify({ model: 'live-side' }))
    const profPath = path.join(appHome, 'profiles', 'work.json')
    fs.writeFileSync(profPath, JSON.stringify({ model: 'profile-side' }))
    const s = agentswap('save', '--json')
    assert.equal(s.code, 3)
    assert.match(s.out, /conflict/)
    const u = agentswap('use', 'personal', '--json')
    assert.equal(u.code, 3)
    assert.match(u.out, /conflict/)
    const forced = agentswapJson('save', '--overwrite-profile', '--json')
    assert.equal(forced.saved, 'work')
    assert.equal(JSON.parse(fs.readFileSync(profPath, 'utf8')).model, 'live-side')
  })

  test('show masks by default; --reveal shows values', () => {
    const profPath = path.join(appHome, 'profiles', 'work.json')
    fs.writeFileSync(profPath, JSON.stringify({ env: { ANTHROPIC_AUTH_TOKEN: 'sk-aaaaaaaaaaaaaaaaaaaaaaaa' } }))
    const masked = agentswapJson('show', 'work', '--json')
    assert.match(masked.settings.env.ANTHROPIC_AUTH_TOKEN, /\(len=\d+\)$/)
    const full = agentswapJson('show', 'work', '--reveal', '--json')
    assert.equal(full.settings.env.ANTHROPIC_AUTH_TOKEN, 'sk-aaaaaaaaaaaaaaaaaaaaaaaa')
  })

  test('diff reports differences', () => {
    const r = agentswapJson('diff', 'work', 'personal', '--json')
    assert.ok(r.changes.length > 0)
    assert.equal(agentswap('diff', 'work', 'work').out.trim(), '(no differences)')
  })

  test('diff never prints a secret inside a one-sided subtree', () => {
    const secret = 'sk-ant-SECRETSECRETSECRET1234'
    const dir = path.join(appHome, 'profiles')
    fs.mkdirSync(dir, { recursive: true })
    fs.writeFileSync(path.join(dir, 'leak-a.json'), JSON.stringify({ env: { ANTHROPIC_AUTH_TOKEN: secret } }))
    fs.writeFileSync(path.join(dir, 'leak-b.json'), JSON.stringify({ model: 'x' }))
    try {
      const human = agentswap('diff', 'leak-a', 'leak-b')
      assert.equal(human.code, 0)
      assert.match(human.out, /@@ env @@/)
      assert.ok(!human.out.includes(secret), `secret leaked: ${human.out}`)
      const json = agentswap('diff', 'leak-a', 'leak-b', '--json')
      assert.ok(!json.out.includes(secret), `secret leaked in --json: ${json.out}`)
    } finally {
      fs.rmSync(path.join(dir, 'leak-a.json'), { force: true })
      fs.rmSync(path.join(dir, 'leak-b.json'), { force: true })
    }
  })

  test('run --json-cmd emits contract without secrets or temp files', () => {
    const r = agentswapJson('run', 'work', '--json-cmd')
    assert.equal(r.command, 'agentswap run work')
    assert.ok(r.args.includes('--settings'))
    assert.ok(r.envKeys.includes('ANTHROPIC_AUTH_TOKEN'))
    const runDir = path.join(appHome, 'run')
    assert.ok(!fs.existsSync(runDir) || fs.readdirSync(runDir).length === 0)
  })

  test('import: dry-run by default, --apply writes; refuses to overwrite', () => {
    const exportFile = path.join(tmp, 'export.json')
    fs.writeFileSync(exportFile, JSON.stringify({
      profiles: [
        { name: 'imported', settings: { model: 'mx', env: { ANTHROPIC_AUTH_TOKEN: 'sk-imported' } } },
        { name: 'work', settings: { model: 'different' } },
      ],
    }))
    const dry = agentswap('import', exportFile, '--json')
    assert.equal(dry.code, 1) // blocking
    assert.match(dry.out, /conflict/)
    fs.writeFileSync(exportFile, JSON.stringify({
      profiles: [{ name: 'imported', settings: { model: 'mx' } }],
    }))
    const applied = agentswapJson('import', exportFile, '--apply', '--json')
    assert.equal(applied.blocking, false)
    assert.ok(fs.existsSync(path.join(appHome, 'profiles', 'imported.json')))
  })

  test('rm deletes; active requires --force', () => {
    assert.equal(agentswap('rm', 'work').code, 1) // active
    const r = agentswapJson('rm', 'personal', '--json')
    assert.equal(r.removed, 'personal')
    assert.equal(agentswap('rm', 'nope').code, 1)
  })

  test('doctor exits 0 on a healthy setup', () => {
    const r = agentswap('doctor')
    assert.equal(r.code, 0, r.out)
    const agent = agentswapJson('doctor', '--json').checks.find(c => c.name === 'agent CLI')
    assert.equal(agent.level, 'ok', agent.detail)
  })

  test('edit launches $EDITOR by bare name via PATH and by a quoted path with spaces', () => {
    // A fake editor that stamps the file it is given. On Windows it is a .cmd
    // shim — what VS Code (code.cmd) and npm-installed editors look like —
    // which spawn cannot run by bare name without resolving it first.
    const edDir = path.join(tmp, 'my editors')
    fs.mkdirSync(edDir, { recursive: true })
    const script = path.join(edDir, 'fake-ed.js')
    fs.writeFileSync(script, 'const fs=require("fs");const f=process.argv[process.argv.length-1];' +
      'const s=JSON.parse(fs.readFileSync(f,"utf8"));s.editedBy=process.argv[2];fs.writeFileSync(f,JSON.stringify(s))\n')
    let exe
    if (process.platform === 'win32') {
      exe = path.join(edDir, 'fake-ed.cmd')
      fs.writeFileSync(exe, `@"${process.execPath}" "%~dp0fake-ed.js" %*\r\n`)
    } else {
      exe = path.join(edDir, 'fake-ed')
      fs.writeFileSync(exe, `#!/bin/sh\nexec "${process.execPath}" "${script}" "$@"\n`)
      fs.chmodSync(exe, 0o755)
    }
    const dir = path.join(appHome, 'profiles')
    fs.mkdirSync(dir, { recursive: true })
    const prof = path.join(dir, 'to-edit.json')
    fs.writeFileSync(prof, '{}')
    try {
      const run = (editor, extraPath = '') => spawnSync(process.execPath, [bin, 'edit', 'to-edit'], {
        env: { ...env(), EDITOR: editor, VISUAL: editor, PATH: `${extraPath}${path.delimiter}${env().PATH}` }, encoding: 'utf8',
      })
      let r = run('fake-ed --by-name', edDir)
      assert.equal(r.status, 0, r.stderr)
      assert.equal(JSON.parse(fs.readFileSync(prof, 'utf8')).editedBy, '--by-name')
      r = run(`"${exe}" --by-path`)
      assert.equal(r.status, 0, r.stderr)
      assert.equal(JSON.parse(fs.readFileSync(prof, 'utf8')).editedBy, '--by-path')
    } finally {
      fs.rmSync(prof, { force: true })
    }
  })

  test('doctor reports a missing agent CLI as not found, not as missing flags', () => {
    const emptyBin = fs.mkdtempSync(path.join(tmp, 'no-agent-'))
    const r = spawnSync(process.execPath, [bin, 'doctor', '--json'], {
      env: { ...env(), PATH: emptyBin }, encoding: 'utf8',
    })
    const agent = JSON.parse(r.stdout).checks.find(c => c.name === 'agent CLI')
    assert.equal(agent.level, 'warn', agent.detail)
    assert.match(agent.detail, /not found/)
  })

  test('help --json returns the machine-readable command schema', () => {
    const r = agentswapJson('help', '--json')
    const names = r.commands.map(c => c.name)
    for (const expected of ['use', 'run', 'push', 'pull', 'sync', 'remote', 'doctor']) {
      assert.ok(names.includes(expected), `missing command: ${expected}`)
    }
    for (const c of r.commands) {
      assert.ok(typeof c.usage === 'string' && c.usage.length > 0, c.name)
      assert.ok(Array.isArray(c.options), c.name)
      assert.ok(Array.isArray(c.arguments), c.name)
      assert.ok(Array.isArray(c.examples), c.name)
    }
    assert.ok(Array.isArray(r.modes) && r.modes.length > 0)
  })

  test('help <cmd> --json and <cmd> --help --json return one command spec', () => {
    const a = agentswapJson('help', 'use', '--json')
    assert.equal(a.command.name, 'use')
    assert.ok(a.command.usage.includes('agentswap use'))
    const b = agentswapJson('use', '--help', '--json')
    assert.equal(b.command.name, 'use')
    assert.equal(agentswap('help', 'nope', '--json').code, 1)
  })

  test('usage errors honor --json with a stable envelope', () => {
    const r = agentswap('use', '--json')
    assert.equal(r.code, 1)
    const parsed = JSON.parse(r.out)
    assert.equal(parsed.version, 2)
    assert.equal(parsed.error, 'usage')
    assert.ok(parsed.usage.includes('agentswap use'))
    assert.ok(parsed.hint.includes('help use --json'))
  })

  test('thrown errors honor --json with a stable envelope', () => {
    const r = agentswap('use', '../evil', '--json')
    assert.equal(r.code, 1)
    const parsed = JSON.parse(r.out)
    assert.equal(parsed.version, 2)
    assert.equal(parsed.error, 'EBADPROFILENAME')
  })

  test('unknown profile suggests close commands too', () => {
    const r = agentswap('usse', '--json')
    assert.equal(r.code, 1)
    assert.match(r.err, /did you mean the command: agentswap use\?/)
  })

  test('empty profile applied to empty live settings reports in-sync', () => {
    fs.writeFileSync(settingsPath, JSON.stringify({}))
    agentswapJson('add', 'blank', '--from-current', '--json')
    agentswapJson('use', 'blank', '--discard', '--json')
    const cur = agentswapJson('current', '--json')
    assert.equal(cur.status, 'in-sync')
    assert.equal(cur.unsaved, false)
  })

  test('journal recovery completes an interrupted switch', () => {
    // Simulate a crash between the settings write and the state commit:
    // live already matches work's content, journal says "→ work", state is gone.
    const work = JSON.parse(fs.readFileSync(path.join(appHome, 'profiles', 'work.json'), 'utf8'))
    fs.writeFileSync(settingsPath, JSON.stringify(work))
    writeJournal({ from: null, to: 'work', oldHash: null, newHash: contentHash(work) })
    fs.rmSync(path.join(appHome, 'state.json'))
    const r = agentswapJson('use', 'work', '--json')
    assert.equal(r.to, 'work')
    const state = JSON.parse(fs.readFileSync(path.join(appHome, 'state.json'), 'utf8'))
    assert.equal(state.activeProfile, 'work')
    assert.ok(!fs.existsSync(journalPath()))
  })

  test('restore refuses files outside the backups pool', () => {
    const before = fs.readFileSync(settingsPath, 'utf8')
    const fake = path.join(tmp, 'fake.json')
    fs.writeFileSync(fake, JSON.stringify({ injected: true }))
    const r = agentswap('restore', fake, '--json')
    assert.equal(r.code, 1)
    assert.match(r.out, /outside the backups pool|restore-outside/)
    assert.equal(fs.readFileSync(settingsPath, 'utf8'), before)
  })

  test('restore refuses a backup whose JSON is not a valid settings object', () => {
    // The profile backup pool also holds run-session quarantine copies, which
    // can be arrays/null — restoring one must not install an invalid root.
    const dir = path.join(appHome, 'backups', 'profiles', 'work')
    fs.mkdirSync(dir, { recursive: true })
    const bad = path.join(dir, '2026-08-21T00-00-00-000Z-0-aaaaaa.json')
    fs.writeFileSync(bad, '[1,2,3]')
    const before = fs.readFileSync(path.join(appHome, 'profiles', 'work.json'), 'utf8')
    const r = agentswap('restore', bad, '--json')
    assert.equal(r.code, 1)
    assert.match(r.out, /not a valid settings object/)
    assert.equal(fs.readFileSync(path.join(appHome, 'profiles', 'work.json'), 'utf8'), before)
  })

  test('restore is a dry run without --yes; --yes writes and backs up the target', () => {
    const dir = path.join(appHome, 'backups', 'settings')
    fs.mkdirSync(dir, { recursive: true })
    const backup = path.join(dir, 'settings-2026-08-21T00-00-00-000Z-0-bbbbbb.json')
    fs.writeFileSync(backup, JSON.stringify({ model: 'restored' }))
    const before = fs.readFileSync(settingsPath, 'utf8')
    try {
      const dry = agentswapJson('restore', backup, '--json')
      assert.equal(dry.dryRun, true)
      assert.equal(dry.target, settingsPath)
      assert.equal(fs.readFileSync(settingsPath, 'utf8'), before, 'dry run must not write')
      assert.match(agentswap('restore', backup).out, /--yes/)
      assert.equal(fs.readFileSync(settingsPath, 'utf8'), before, 'dry run must not write')

      const settingsBackups = () => fs.readdirSync(dir).length
      const n = settingsBackups()
      const real = agentswapJson('restore', backup, '--yes', '--json')
      assert.equal(real.dryRun, false)
      assert.equal(real.restored, settingsPath)
      assert.deepEqual(JSON.parse(fs.readFileSync(settingsPath, 'utf8')), { model: 'restored' })
      assert.equal(settingsBackups(), n + 1, 'the replaced settings are backed up first')
    } finally {
      fs.writeFileSync(settingsPath, before)
      fs.rmSync(backup, { force: true })
    }
  })

  test('import rejects invalid entry names without writing anything', () => {
    const exportFile = path.join(tmp, 'bad-export.json')
    fs.writeFileSync(exportFile, JSON.stringify({ profiles: [
      { name: '../evil', settings: { model: 'x' } },
      { name: 'fine', settings: { model: 'y' } },
    ] }))
    const r = agentswap('import', exportFile, '--apply', '--json')
    assert.equal(r.code, 1)
    assert.ok(!fs.existsSync(path.join(appHome, 'profiles', 'fine.json')))
  })

  test('import refuses a file that names the same profile twice', () => {
    const exportFile = path.join(tmp, 'dup-export.json')
    fs.writeFileSync(exportFile, JSON.stringify({ profiles: [
      { name: 'dup-one', settings: { model: 'first' } },
      { name: 'dup-one', settings: { model: 'second' } },
    ] }))
    const r = agentswap('import', exportFile, '--apply', '--json')
    assert.equal(r.code, 1)
    const out = JSON.parse(r.out)
    assert.equal(out.blocking, true)
    assert.deepEqual(out.profiles.map(p => p.status), ['duplicate', 'duplicate'])
    assert.ok(!fs.existsSync(path.join(appHome, 'profiles', 'dup-one.json')), 'nothing may be written')
  })

  test('import never overwrites a profile whose name differs only in case', () => {
    const dir = path.join(appHome, 'profiles')
    fs.mkdirSync(dir, { recursive: true })
    fs.writeFileSync(path.join(dir, 'casey.json'), JSON.stringify({ model: 'original' }))
    const exportFile = path.join(tmp, 'case-export.json')
    fs.writeFileSync(exportFile, JSON.stringify({ profiles: [{ name: 'Casey', settings: { model: 'imported' } }] }))
    try {
      // One file on macOS/Windows; refused on Linux too, so the profile set
      // stays portable to the machines it syncs with.
      const r = agentswap('import', exportFile, '--apply', '--json')
      const out = JSON.parse(r.out)
      assert.equal(r.code, 1)
      assert.equal(out.profiles[0].status, 'conflict')
      assert.match(out.profiles[0].detail, /"casey"/)
      assert.deepEqual(JSON.parse(fs.readFileSync(path.join(dir, 'casey.json'), 'utf8')), { model: 'original' })
      assert.ok(fs.readdirSync(dir).includes('casey.json'), 'the original file must keep its name')
    } finally {
      for (const f of fs.readdirSync(dir)) if (f.toLowerCase() === 'casey.json') fs.rmSync(path.join(dir, f), { force: true })
    }
  })

  test('profile names resolve exactly and stay unique ignoring case, on every platform', () => {
    const dir = path.join(appHome, 'profiles')
    fs.mkdirSync(dir, { recursive: true })
    fs.writeFileSync(path.join(dir, 'exact.json'), JSON.stringify({ model: 'exact' }))
    try {
      // resolution: 'EXACT' is not 'exact', even where the filesystem would open it
      assert.equal(agentswap('show', 'EXACT').code, 1)
      assert.equal(agentswap('diff', 'EXACT', 'exact').code, 1)
      assert.equal(agentswap('use', 'EXACT', '--json').code, 1)
      assert.equal(agentswap('run', 'EXACT', '--json-cmd').code, 1)
      const rmr = agentswap('rm', 'EXACT', '--force', '--json')
      assert.equal(rmr.code, 1)
      assert.equal(JSON.parse(rmr.out).error, 'not-found')
      assert.ok(fs.existsSync(path.join(dir, 'exact.json')), 'rm of a case variant must not delete the profile')
      // uniqueness: a NEW name that only differs in case is refused
      const add = agentswap('add', 'Exact', '--from-current', '--json')
      assert.equal(add.code, 1)
      assert.equal(JSON.parse(add.out).error, 'name-exists')
      assert.deepEqual(fs.readdirSync(dir).filter(f => f.toLowerCase() === 'exact.json'), ['exact.json'])
      assert.deepEqual(JSON.parse(fs.readFileSync(path.join(dir, 'exact.json'), 'utf8')), { model: 'exact' })
    } finally {
      fs.rmSync(path.join(dir, 'exact.json'), { force: true })
    }
  })

  test('import treats names differing only in case as duplicates on every platform', () => {
    const exportFile = path.join(tmp, 'case-dup-export.json')
    fs.writeFileSync(exportFile, JSON.stringify({ profiles: [
      { name: 'mixed', settings: { model: 'a' } },
      { name: 'MIXED', settings: { model: 'b' } },
    ] }))
    const r = agentswap('import', exportFile, '--apply', '--json')
    assert.equal(r.code, 1)
    assert.deepEqual(JSON.parse(r.out).profiles.map(p => p.status), ['duplicate', 'duplicate'])
    assert.ok(!fs.readdirSync(path.join(appHome, 'profiles')).some(f => f.toLowerCase() === 'mixed.json'))
  })

  test('import accepts only the {profiles:[{name,settings}]} shape', () => {
    // The bare-array and name→settings-map shapes are no longer guessed.
    for (const shape of [
      [{ name: 'x', settings: { model: 'm' } }],
      { x: { model: 'm' } },
    ]) {
      const f = path.join(tmp, 'shape.json')
      fs.writeFileSync(f, JSON.stringify(shape))
      const r = agentswap('import', f, '--json')
      assert.equal(r.code, 1)
      assert.match(r.out, /invalid-shape|expected/)
    }
  })

  test('run refuses a non-object profile with a clean error (no TypeError)', () => {
    const bad = path.join(appHome, 'profiles', 'bad.json')
    for (const content of ['null', '[1,2,3]', '"scalar"']) {
      fs.writeFileSync(bad, content)
      const r = agentswap('run', 'bad', '--json-cmd')
      assert.equal(r.code, 1, `expected refusal for ${content}`)
      assert.match(r.out, /invalid profile/)
      assert.ok(!/TypeError/.test(r.out + r.err))
    }
    fs.rmSync(bad, { force: true })
  })

  test('journal recovery fails closed (EJOURNAL) when live is unreadable', () => {
    const work = JSON.parse(fs.readFileSync(path.join(appHome, 'profiles', 'work.json'), 'utf8'))
    writeJournal({ from: null, to: 'work', oldHash: null, newHash: contentHash(work) })
    fs.writeFileSync(settingsPath, '{not json')
    const r = agentswap('use', 'work', '--json')
    assert.equal(r.code, 1)
    assert.match(r.err, /interrupted switch/)
    assert.ok(fs.existsSync(journalPath()), 'journal must be preserved as evidence')
    // restore a usable state for the remaining tests
    fs.writeFileSync(settingsPath, JSON.stringify(work))
    fs.rmSync(journalPath(), { force: true })
  })

  test('use <active> re-materializes live even when the profile was edited (conflict)', () => {
    // live missing AND the active profile edited → reconcile says conflict,
    // but there is nothing to keep: apply the edited profile, don't block.
    fs.rmSync(settingsPath)
    const profPath = path.join(appHome, 'profiles', 'work.json')
    fs.writeFileSync(profPath, JSON.stringify({ model: 'edited-profile' }))
    const r = agentswapJson('use', 'work', '--json')
    assert.equal(r.to, 'work')
    assert.deepEqual(JSON.parse(fs.readFileSync(settingsPath, 'utf8')), { model: 'edited-profile' })
    assert.equal(agentswapJson('current', '--json').status, 'in-sync')
  })

  test('use <other> --overwrite-profile with missing live does not write null into the active profile', () => {
    agentswapJson('add', 'other', '--from-current', '--json')
    fs.rmSync(settingsPath)
    const workBefore = fs.readFileSync(path.join(appHome, 'profiles', 'work.json'), 'utf8')
    const r = agentswapJson('use', 'other', '--overwrite-profile', '--json')
    assert.equal(r.to, 'other')
    assert.equal(fs.readFileSync(path.join(appHome, 'profiles', 'work.json'), 'utf8'), workBefore,
      'missing live must not be saved into the active profile')
    assert.ok(fs.existsSync(settingsPath), 'live settings.json should be re-materialized')
  })

  test('save with a missing live file errors instead of claiming in-sync', () => {
    fs.rmSync(settingsPath)
    const r = agentswap('save', '--json')
    assert.equal(r.code, 1)
    assert.match(r.out, /live-missing/)
    // and re-applying the profile restores a consistent world
    const u = agentswapJson('use', 'work', '--discard', '--json')
    assert.equal(u.to, 'work')
    assert.equal(agentswapJson('current', '--json').status, 'in-sync')
  })

  test('doctor fails on a non-object live settings root', () => {
    fs.writeFileSync(settingsPath, '[1,2,3]')
    const r = agentswap('doctor', '--json')
    assert.equal(r.code, 1)
    const report = JSON.parse(r.out)
    const settingsCheck = report.checks.find((c) => c.name === 'settings.json')
    assert.equal(settingsCheck.level, 'fail')
    // restore
    const work = JSON.parse(fs.readFileSync(path.join(appHome, 'profiles', 'work.json'), 'utf8'))
    fs.writeFileSync(settingsPath, JSON.stringify(work))
  })

  test('use <active> re-applies the profile when the live file is missing', () => {
    fs.rmSync(settingsPath)
    const r = agentswapJson('use', 'work', '--json')
    assert.equal(r.to, 'work')
    assert.ok(fs.existsSync(settingsPath), 'live settings.json should be re-materialized')
    assert.equal(agentswapJson('current', '--json').status, 'in-sync')
  })

  test('use <active> --overwrite-profile converges live and profile', () => {
    // conflict on the active profile: live changed AND the profile changed
    fs.writeFileSync(settingsPath, JSON.stringify({ model: 'live-side' }))
    const profPath = path.join(appHome, 'profiles', 'work.json')
    fs.writeFileSync(profPath, JSON.stringify({ model: 'profile-side' }))
    const r = agentswapJson('use', 'work', '--overwrite-profile', '--json')
    assert.equal(r.to, 'work')
    // the profile now holds live; live was NOT reverted to the old profile bytes
    assert.deepEqual(JSON.parse(fs.readFileSync(profPath, 'utf8')), { model: 'live-side' })
    assert.deepEqual(JSON.parse(fs.readFileSync(settingsPath, 'utf8')), { model: 'live-side' })
    assert.equal(agentswapJson('current', '--json').status, 'in-sync')
  })

  // A fake agent on PATH: overwrites its --settings file (content driven by
  // FAKE_AGENT_WRITE), optionally edits the profile first to simulate a
  // concurrent edit, or deletes the settings file outright. Lets us exercise
  // run's post-exit handling end-to-end.
  function fakeAgentEnv({ write, profileEdit = null, deleteSettings = false, chmodSettings = null }) {
    const binDir = path.join(tmp, `fakebin-${Math.random().toString(36).slice(2)}`)
    fs.mkdirSync(binDir, { recursive: true })
    const profPath = path.join(appHome, 'profiles', 'work.json')
    const script = '#!/usr/bin/env node\n' +
      'const fs=require("fs");\n' +
      `if (${JSON.stringify(profileEdit !== null)}) fs.writeFileSync(${JSON.stringify(profPath)}, ${JSON.stringify(profileEdit ?? '')});\n` +
      'const i=process.argv.indexOf("--settings");\n' +
      `if (i>=0) { ${deleteSettings ? 'fs.unlinkSync(process.argv[i+1]);' : `fs.writeFileSync(process.argv[i+1], ${JSON.stringify(write)});`} ${chmodSettings != null ? `fs.chmodSync(process.argv[i+1], ${chmodSettings});` : ''} }\n`
    fs.writeFileSync(path.join(binDir, 'claude'), script)
    fs.chmodSync(path.join(binDir, 'claude'), 0o755)
    if (process.platform === 'win32') {
      // The layout a global npm install leaves: the extensionless sh shim
      // (which `where` lists first), a .ps1, and the .cmd shim that runs the
      // package's JS entry.
      const entry = path.join(binDir, 'node_modules', 'fake-agent', 'cli.js')
      fs.mkdirSync(path.dirname(entry), { recursive: true })
      fs.writeFileSync(entry, script)
      fs.writeFileSync(path.join(binDir, 'claude.ps1'), '# not launchable without powershell\r\n')
      fs.writeFileSync(path.join(binDir, 'claude.cmd'),
        '@ECHO off\r\nSETLOCAL\r\nSET "_prog=node"\r\n' +
        'endLocal & goto #_undefined_# 2>NUL || title %COMSPEC% & "%_prog%"  "%dp0%\\node_modules\\fake-agent\\cli.js" %*\r\n')
    }
    return { ...env(), PATH: `${binDir}${path.delimiter}${process.env.PATH}` }
  }

  test('run --save-on-exit backs up and exits non-zero when the agent writes garbage', () => {
    const r = spawnSync(process.execPath, [bin, 'run', 'work', '--save-on-exit'], {
      env: fakeAgentEnv({ write: 'GARBAGE{' }), encoding: 'utf8',
    })
    assert.notEqual(r.status, 0, 'expected non-zero: save-on-exit could not save')
    assert.match(r.stderr, /unparseable settings/)
    const backupDir = path.join(appHome, 'backups', 'profiles', 'work')
    assert.ok(fs.existsSync(backupDir) && fs.readdirSync(backupDir).length > 0, 'garbage content was backed up')
  })

  test('run --save-on-exit exits non-zero on a concurrent profile edit (conflict)', () => {
    const before = fs.readdirSync(path.join(appHome, 'backups', 'profiles', 'work')).length
    const r = spawnSync(process.execPath, [bin, 'run', 'work', '--save-on-exit'], {
      env: fakeAgentEnv({ write: JSON.stringify({ model: 'agent-wrote' }), profileEdit: JSON.stringify({ model: 'concurrent-edit' }) }),
      encoding: 'utf8',
    })
    assert.notEqual(r.status, 0, 'expected non-zero: conflict — run changes not saved')
    assert.match(r.stderr, /conflict/)
    const after = fs.readdirSync(path.join(appHome, 'backups', 'profiles', 'work')).length
    assert.ok(after > before, 'the lost change was backed up')
    // the concurrent edit wins the profile
    assert.deepEqual(JSON.parse(fs.readFileSync(path.join(appHome, 'profiles', 'work.json'), 'utf8')), { model: 'concurrent-edit' })
  })

  test('run --save-on-exit exits non-zero when the agent deleted the settings file', () => {
    const r = spawnSync(process.execPath, [bin, 'run', 'work', '--save-on-exit'], {
      env: fakeAgentEnv({ write: '', deleteSettings: true }), encoding: 'utf8',
    })
    assert.notEqual(r.status, 0, 'expected non-zero: nothing to save')
    assert.match(r.stderr, /could not read the session settings/)
  })

  test('run --save-on-exit succeeds when the profile was concurrently edited to the same content', () => {
    const same = JSON.stringify({ model: 'agent-wrote' })
    const r = spawnSync(process.execPath, [bin, 'run', 'work', '--save-on-exit'], {
      env: fakeAgentEnv({ write: same, profileEdit: same }), encoding: 'utf8',
    })
    assert.equal(r.status, 0, `expected success — the change is already in the profile: ${r.stderr}`)
  })

  test('run --save-on-exit leaves the temp file in place when it cannot be read', {
    skip: process.platform === 'win32'
      ? 'win32 has no POSIX file modes: chmod 000 leaves the file readable'
      : typeof process.getuid === 'function' && process.getuid() === 0
        ? 'root bypasses file permissions (chmod 000 is readable)' : false,
  }, () => {
    const before = fs.readdirSync(path.join(appHome, 'run')).length
    const r = spawnSync(process.execPath, [bin, 'run', 'work', '--save-on-exit'], {
      env: fakeAgentEnv({ write: JSON.stringify({ model: 'agent-wrote' }), chmodSettings: 0o000 }),
      encoding: 'utf8',
    })
    assert.notEqual(r.status, 0, 'expected non-zero: nothing saved')
    assert.match(r.stderr, /could not read the session settings/)
    const after = fs.readdirSync(path.join(appHome, 'run'))
    assert.ok(after.length > before, 'the unreadable temp file must be left in place, not deleted')
    assert.ok(after.some(f => r.stderr.includes(f)), 'the error must name the file it kept')
    // The session that wrote it has exited, so its pid is dead: the NEXT run's
    // owner-pid GC must still leave the kept file alone — it may be the only copy.
    const next = spawnSync(process.execPath, [bin, 'run', 'work'], {
      env: fakeAgentEnv({ write: fs.readFileSync(path.join(appHome, 'profiles', 'work.json'), 'utf8') }),
      encoding: 'utf8',
    })
    assert.equal(next.status, 0, next.stderr)
    assert.deepEqual(fs.readdirSync(path.join(appHome, 'run')), after, 'the next run must not collect the kept file')
    const leftovers = agentswapJson('doctor', '--json').checks.find(c => c.name === 'leftovers')
    assert.match(leftovers.detail, /kept after a failed --save-on-exit/, 'doctor must not call a kept file safe to delete')
    for (const f of after) {
      const p = path.join(appHome, 'run', f)
      fs.chmodSync(p, 0o600)
      fs.rmSync(p, { force: true })
    }
  })
})
