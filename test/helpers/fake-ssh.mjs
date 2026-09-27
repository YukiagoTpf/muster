import fs from 'node:fs'
import path from 'node:path'

/**
 * Create an executable `ssh` shim in `dir` that maps host aliases to
 * AGENTSWAP_HOME directories and execs the remote command there.
 *
 * Homes are baked into the script as a JSON literal at install time — the
 * helper cannot set env for later spawns. Prepend the returned dir to PATH.
 *
 * The shim is CJS (extensionless files are CJS). stdio inherit so piped
 * stdin (a write's JSON envelope) flows through to the remote command.
 *
 * @param {string} dir
 * @param {Record<string, string>} homes host alias → agentswap home path
 * @returns {string} dir
 */
export function installFakeSsh(dir, homes) {
  fs.mkdirSync(dir, { recursive: true })
  const dest = path.join(dir, 'ssh')
  // Concatenation, not a template literal: a home path containing `${` would
  // otherwise interpolate in THIS process before the shim is written.
  const script = [
    '#!/usr/bin/env node',
    "'use strict'",
    "const { spawnSync } = require('child_process')",
    'const homes = ' + JSON.stringify(homes),
    'const args = process.argv.slice(2)',
    'const command = args[args.length - 1]',
    'const host = args[args.length - 2]',
    'const home = homes[host]',
    'if (!home) {',
    "  process.stderr.write('fake-ssh: unknown host: ' + host + '\\n')",
    '  process.exit(255)',
    '}',
    "const r = spawnSync('sh', ['-c', command], {",
    "  stdio: 'inherit',",
    '  env: Object.assign({}, process.env, { AGENTSWAP_HOME: home }),',
    '})',
    'if (r.error) {',
    '  process.stderr.write(String(r.error.message) + "\\n")',
    '  process.exit(1)',
    '}',
    'process.exit(r.status == null ? 1 : r.status)',
    '',
  ].join('\n')
  fs.writeFileSync(dest, script)
  fs.chmodSync(dest, 0o755)
  return dir
}
