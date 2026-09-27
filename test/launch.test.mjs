import { test } from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { isAuthRoutingEnvKey, launchEnv, resolveAgentCommand, quoteCmdArg, cmdCommandLine, resolveShimJsEntry, parseShimJsEntry } from '../src/launch.mjs'
import { splitCommandLine } from '../src/editor.mjs'

test('isAuthRoutingEnvKey covers ANTHROPIC_* and auth-ish CLAUDE_CODE_*', () => {
  assert.ok(isAuthRoutingEnvKey('ANTHROPIC_AUTH_TOKEN'))
  assert.ok(isAuthRoutingEnvKey('ANTHROPIC_BASE_URL'))
  assert.ok(isAuthRoutingEnvKey('ANTHROPIC_MODEL'))
  assert.ok(isAuthRoutingEnvKey('CLAUDE_CODE_OAUTH_TOKEN'))
  assert.ok(isAuthRoutingEnvKey('CLAUDE_CODE_API_KEY'))
  assert.ok(!isAuthRoutingEnvKey('CLAUDE_CODE_MAX_OUTPUT_TOKENS'))
  assert.ok(!isAuthRoutingEnvKey('PATH'))})

test('launchEnv strips inherited auth and injects profile env', () => {
  const base = {
    ANTHROPIC_AUTH_TOKEN: 'stale',
    ANTHROPIC_BASE_URL: 'https://stale.example.com',
    CLAUDE_CODE_OAUTH_TOKEN: 'oauth-stale',
    CLAUDE_CODE_MAX_OUTPUT_TOKENS: '8192',
    PATH: '/usr/bin',
  }
  const env = launchEnv(
    { ANTHROPIC_BASE_URL: 'https://fresh.example.com', CUSTOM: 'x' },
    { baseEnv: base },
  )
  assert.equal(env.ANTHROPIC_AUTH_TOKEN, undefined) // stripped, not re-injected
  assert.equal(env.ANTHROPIC_BASE_URL, 'https://fresh.example.com')
  assert.equal(env.CLAUDE_CODE_OAUTH_TOKEN, undefined)
  assert.equal(env.CLAUDE_CODE_MAX_OUTPUT_TOKENS, '8192') // non-auth kept
  assert.equal(env.PATH, '/usr/bin')
  assert.equal(env.CUSTOM, 'x')
})

test('launchEnv honors unsetEnv and null deletes', () => {
  const env = launchEnv(
    { KEEP: '1', DROP: null },
    { baseEnv: { KEEP: '0', DROP: 'x', EXTRA: 'y' }, unsetEnv: ['EXTRA'] },
  )
  assert.equal(env.KEEP, '1')
  assert.equal(env.DROP, undefined)
  assert.equal(env.EXTRA, undefined)
})

test('resolveAgentCommand: posix uses bare name; win32 cmd shim gets cmd.exe wrapper', () => {
  assert.deepEqual(resolveAgentCommand('darwin', null), { cmd: 'claude', verbatim: false, shim: null })
  const win = resolveAgentCommand('win32', 'C:\\Users\\me\\AppData\\Roaming\\npm\\claude.cmd\r\n')
  assert.equal(win.cmd, 'cmd.exe')
  assert.equal(win.verbatim, true)
  assert.ok(win.shim.endsWith('claude.cmd'))
  const exe = resolveAgentCommand('win32', 'C:\\tools\\claude.exe\r\n')
  assert.equal(exe.cmd, 'C:\\tools\\claude.exe')
  assert.equal(exe.shim, null)
})

test('resolveAgentCommand: win32 skips npm\'s extensionless sh shim and .ps1 that `where` lists first', () => {
  // A global npm install drops claude (sh), claude.cmd and claude.ps1 side by
  // side, and `where claude` prints the extensionless one FIRST.
  const npmDir = 'C:\\Users\\me\\AppData\\Roaming\\npm'
  const where = [`${npmDir}\\claude`, `${npmDir}\\claude.ps1`, `${npmDir}\\claude.cmd`, 'C:\\tools\\claude.exe', ''].join('\r\n')
  const r = resolveAgentCommand('win32', where)
  assert.equal(r.cmd, 'cmd.exe')
  assert.equal(r.shim, `${npmDir}\\claude.cmd`)
  // nothing launchable at all → the bare name (spawn reports ENOENT honestly)
  assert.deepEqual(resolveAgentCommand('win32', `${npmDir}\\claude\r\n`), { cmd: 'claude', verbatim: false, shim: null })
})

test('quoteCmdArg: spaces and cmd metacharacters are quoted; plain args pass through', () => {
  assert.equal(quoteCmdArg('--dangerously-skip-permissions'), '--dangerously-skip-permissions')
  assert.equal(quoteCmdArg(''), '""')
  assert.equal(quoteCmdArg('C:\\Program Files\\node'), '"C:\\Program Files\\node"')
  // metacharacters must not be interpretable by cmd
  assert.match(quoteCmdArg('a & b'), /^"a & b"$/)
  assert.match(quoteCmdArg('a|b'), /^"a\|b"$/)
  assert.equal(quoteCmdArg('say "hi"'), '"say \\"hi\\""')
})

test('cmdCommandLine wraps the whole line in one outer quote pair', () => {
  const line = cmdCommandLine('C:\\npm\\claude.cmd', ['--settings', 'C:\\my dir\\s.json'])
  assert.ok(line.startsWith('"') && line.endsWith('"'))
  assert.ok(line.includes('C:\\npm\\claude.cmd --settings'))
  assert.ok(line.includes('"C:\\my dir\\s.json"'))
})

test('parseShimJsEntry extracts entry and interpreter args from npm .cmd shims', () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agentswap-shim-'))
  const shim = path.join(dir, 'claude.cmd')
  // modern npm shim shape
  fs.writeFileSync(shim, '@ECHO off\r\nSETLOCAL\r\n...\r\n"%_prog%"  "%dp0%\\node_modules\\foo\\bin\\foo.js" %*\r\n')
  assert.deepEqual(parseShimJsEntry(shim), { entry: '%dp0%\\node_modules\\foo\\bin\\foo.js', interpreterArgs: [] })
  // npm's official cmd-shim shape: node flags between interpreter and entry
  fs.writeFileSync(shim, 'endLocal & goto #_undefined_# 2>NUL || title %COMSPEC% & "%_prog%" --no-warnings=ExperimentalWarning --enable-source-maps "%dp0%\\cli.mjs" %*\r\n')
  assert.deepEqual(parseShimJsEntry(shim), {
    entry: '%dp0%\\cli.mjs',
    interpreterArgs: ['--no-warnings=ExperimentalWarning', '--enable-source-maps'],
  })
  // npm's official shape with a QUOTED node arg: the interpreter must be
  // anchored — matching any quoted token would restart at "./hook.js" and
  // silently drop --require. We can't reproduce a quoted relative arg with a
  // direct node spawn (cwd differs), so this must fall back to cmd.exe.
  fs.writeFileSync(shim, 'endLocal & goto #_undefined_# 2>NUL || title %COMSPEC% & "%_prog%" --require "./hook.js" "%dp0%\\cli.js" %*\r\n')
  assert.equal(parseShimJsEntry(shim), null)
  // a quoted arg VALUE that looks like the interpreter must not re-sync the
  // match: --conditions "node" would otherwise be accepted as the interpreter
  // and silently dropped.
  fs.writeFileSync(shim, 'endLocal & goto #_undefined_# 2>NUL || title %COMSPEC% & "%_prog%" --conditions "node" "%dp0%\\cli.js" %*\r\n')
  assert.equal(parseShimJsEntry(shim), null)
  // legacy %~dp0 shape
  fs.writeFileSync(shim, '"%~dp0\\node.exe" "%~dp0\\node_modules\\foo\\bin\\foo.js" %*\r\n')
  assert.deepEqual(parseShimJsEntry(shim), { entry: '%~dp0\\node_modules\\foo\\bin\\foo.js', interpreterArgs: [] })
  // a non-flag interpreter arg is not a shape we bypass → null (cmd.exe fallback)
  fs.writeFileSync(shim, '"%_prog%" weirdness "%dp0%\\foo.js" %*\r\n')
  assert.equal(parseShimJsEntry(shim), null)
  // %VAR% / metacharacters in the args → null (can't reproduce without cmd.exe)
  fs.writeFileSync(shim, '"%_prog%" --flag=%PATH% "%dp0%\\foo.js" %*\r\n')
  assert.equal(parseShimJsEntry(shim), null)
  // unknown shape → null
  fs.writeFileSync(shim, '@ECHO off\r\nsomething else entirely\r\n')
  assert.equal(parseShimJsEntry(shim), null)
  fs.rmSync(dir, { recursive: true, force: true })
})

test('resolveShimJsEntry resolves entry and args to a real file (posix separators)', () => {
  // npm shims on Windows use backslashes; on POSIX a backslash is a legal
  // filename character, so resolution is exercised with forward separators
  // (which Windows accepts too). The backslash extraction itself is covered
  // by the parseShimJsEntry test above.
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agentswap-shim-'))
  const shim = path.join(dir, 'claude.cmd')
  const jsRel = path.join('node_modules', 'foo', 'bin', 'foo.js')
  fs.mkdirSync(path.join(dir, 'node_modules', 'foo', 'bin'), { recursive: true })
  fs.writeFileSync(path.join(dir, jsRel), '// entry')
  fs.writeFileSync(shim, '"%_prog%"  "%dp0%/node_modules/foo/bin/foo.js" %*\r\n')
  assert.deepEqual(resolveShimJsEntry(shim), { jsPath: path.join(dir, jsRel), interpreterArgs: [] })
  // .mjs entry with node flags
  const mjsRel = path.join('node_modules', 'foo', 'bin', 'bar.mjs')
  fs.writeFileSync(path.join(dir, mjsRel), '// entry')
  fs.writeFileSync(shim, '"%_prog%" --enable-source-maps "%dp0%/node_modules/foo/bin/bar.mjs" %*\r\n')
  assert.deepEqual(resolveShimJsEntry(shim), {
    jsPath: path.join(dir, mjsRel),
    interpreterArgs: ['--enable-source-maps'],
  })
  // missing entry target → null (caller falls back to cmd.exe)
  fs.writeFileSync(shim, '"%_prog%"  "%dp0%/node_modules/ghost/bin/ghost.js" %*\r\n')
  assert.equal(resolveShimJsEntry(shim), null)
  fs.rmSync(dir, { recursive: true, force: true })
})

test('splitCommandLine: whitespace splits, quotes group, backslashes stay literal', () => {
  assert.deepEqual(splitCommandLine('code --wait'), ['code', '--wait'])
  assert.deepEqual(splitCommandLine('  vim   -f '), ['vim', '-f'])
  assert.deepEqual(
    splitCommandLine('"C:\Program Files\Microsoft VS Code\bin\code.cmd" --wait'),
    ['C:\Program Files\Microsoft VS Code\bin\code.cmd', '--wait'])
  assert.deepEqual(splitCommandLine("'/opt/my editor/ed' -n"), ['/opt/my editor/ed', '-n'])
  assert.deepEqual(splitCommandLine('node -e ""'), ['node', '-e', ''])
  assert.deepEqual(splitCommandLine(''), [])
})
