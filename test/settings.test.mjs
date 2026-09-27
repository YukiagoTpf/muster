import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  canonicalize, contentHash, deepEqual, jsonDiff, maskSettings, maskValue,
  isSecretKey, maskDiffLeaf, sanitizeTerminal,
} from '../src/settings.mjs'

test('canonicalize sorts keys but preserves array order', () => {
  assert.equal(canonicalize({ b: 1, a: 2 }), canonicalize({ a: 2, b: 1 }))
  assert.notEqual(canonicalize([1, 2]), canonicalize([2, 1]))
})

test('contentHash is stable and distinct', () => {
  assert.equal(contentHash({ a: 1 }), contentHash({ a: 1 }))
  assert.notEqual(contentHash({ a: 1 }), contentHash({ a: 2 }))
})

test('deepEqual is key-order insensitive, type strict', () => {
  assert.ok(deepEqual({ a: 1, b: 2 }, { b: 2, a: 1 }))
  assert.ok(!deepEqual({ a: 1 }, { a: '1' }))
})

test('jsonDiff reports leaf paths', () => {
  const d = jsonDiff({ env: { A: 1 } }, { env: { A: 2 } })
  assert.equal(d.length, 1)
  assert.equal(d[0].path, 'env.A')
  assert.equal(d[0].a, 1)
  assert.equal(d[0].b, 2)
})

test('maskSettings masks secrets by key name, env contents, and format', () => {
  const s = {
    env: { ANTHROPIC_AUTH_TOKEN: 'sk-abcdefghijklmnopqrstuvwxyz', PLAIN: 'visible' },
    apiKey: 'sk-1234567890abcdefghijklmnop',
    nested: { PASSWORD: 'hunter2hunter2hunter2hunter2' },
  }
  const m = maskSettings(s)
  assert.match(m.env.ANTHROPIC_AUTH_TOKEN, /^sk-…\w+\(len=\d+\)$/)
  assert.match(m.env.PLAIN, /\(len=\d+\)$/) // env container masks all strings
  assert.notEqual(m.env.PLAIN, 'visible')
  assert.match(m.apiKey, /\(len=\d+\)$/)
  assert.match(m.nested.PASSWORD, /\(len=\d+\)$/)
  assert.notEqual(m.nested.PASSWORD, s.nested.PASSWORD)
})

test('camelCase secret key names are recognized', () => {
  assert.ok(isSecretKey('apiKey'))
  assert.ok(isSecretKey('privateKey'))
  assert.ok(isSecretKey('accessKey'))
  assert.ok(isSecretKey('ANTHROPIC_AUTH_TOKEN'))
  assert.ok(!isSecretKey('monkey'))
  assert.ok(!isSecretKey('keyboard'))
  assert.ok(!isSecretKey('model'))
  const m = maskSettings({ apiKey: 'sk-1234567890abcdefghijklmnop' })
  assert.notEqual(m.apiKey, 'sk-1234567890abcdefghijklmnop')
})

test('short secrets are never revealed, even fully', () => {
  for (const short of ['1234', 'abc', 'x', 'sk-ab']) {
    const m = maskSettings({ env: { PIN: short }, password: short })
    assert.ok(!m.env.PIN.includes(short), `env value leaked: ${m.env.PIN}`)
    assert.ok(!m.password.includes(short), `password leaked: ${m.password}`)
  }
})

test('maskDiffLeaf masks by path context; diff never collides two secrets', () => {
  assert.equal(maskDiffLeaf('env.TOKEN', 'sk-abcdefghij1234'), 'sk-…1234(len=17)')
  assert.equal(maskDiffLeaf('apiKey', 'sk-abcdefghij1234'), 'sk-…1234(len=17)')
  assert.equal(maskDiffLeaf('model', 'claude-sonnet-5'), 'claude-sonnet-5')
  // two DIFFERENT secrets, same length + same last 4 → the change is still
  // reported (comparison is on raw values); masked output leaks neither
  const a = { env: { TOKEN: 'sk-aaaaaaaaaaaaaaaaaaaa9999' } }
  const b = { env: { TOKEN: 'sk-bbbbbbbbbbbbbbbbbbbb9999' } }
  const d = jsonDiff(a, b).map(c => ({ ...c, a: maskDiffLeaf(c.path, c.a), b: maskDiffLeaf(c.path, c.b) }))
  assert.equal(d.length, 1)
  assert.notEqual(d[0].a, a.env.TOKEN)
  assert.notEqual(d[0].b, b.env.TOKEN)
})

test('diff masks a whole subtree that exists on one side only', () => {
  // A key present on one side only is ONE diff leaf whose value is the whole
  // subtree — every secret inside it must be masked, not printed raw.
  const secret = 'sk-ant-SECRETSECRETSECRET1234'
  const plain = 'plainsecretvalue'
  const a = {
    env: { ANTHROPIC_AUTH_TOKEN: secret },
    mcpServers: { x: { env: { PIN: plain }, headers: { Authorization: `Bearer ${plain}` }, command: 'npx' } },
    model: 'claude-sonnet-5',
  }
  const d = jsonDiff(a, {}).map(c => ({ ...c, a: maskDiffLeaf(c.path, c.a), b: maskDiffLeaf(c.path, c.b) }))
  const out = JSON.stringify(d)
  assert.ok(!out.includes(secret), `env token leaked: ${out}`)
  assert.ok(!out.includes(plain), `nested secret leaked: ${out}`)
  // non-secret leaves inside a masked subtree stay readable
  assert.equal(d.find(c => c.path === 'mcpServers').a.x.command, 'npx')
  assert.equal(d.find(c => c.path === 'model').a, 'claude-sonnet-5')
})

test('maskValue leaves ordinary strings alone', () => {
  assert.equal(maskValue('hello'), 'hello')
  assert.equal(maskValue('https://api.example.com'), 'https://api.example.com')
})

test('authorization / authentication / oauth key names are secret', () => {
  for (const k of ['authorization', 'Authorization', 'AUTHENTICATION', 'oauth', 'oauth2', 'OAUTH_TOKEN']) {
    assert.ok(isSecretKey(k), `${k} should be secret`)
  }
  assert.ok(!isSecretKey('author'))
  const m = maskSettings({ authorization: 'Basic dXNlcjpwYXNz' })
  assert.notEqual(m.authorization, 'Basic dXNlcjpwYXNz')
  assert.match(m.authorization, /^Basic /)
})

test('hyphen/dot/space-delimited secret key names are recognized', () => {
  for (const k of ['api-key', 'auth-token', 'x.api.key', 'Secret Key', 'client_secret']) {
    assert.ok(isSecretKey(k), `${k} should be secret`)
  }
  const m = maskSettings({ 'api-key': 'sk-1234567890abcdefghijklmnop' })
  assert.notEqual(m['api-key'], 'sk-1234567890abcdefghijklmnop')
})

test('diff masks literal dot-key secrets the same way show does', () => {
  // A literal key 'x.api.key' must not leak via the diff path: the path
  // segments x/api/key are each innocent, but the whole path is a secret name.
  const a = JSON.parse('{"x.api.key":"hunter2hunter"}')
  const b = JSON.parse('{"x.api.key":"hunter3hunter"}')
  const d = jsonDiff(a, b).map(c => ({ ...c, a: maskDiffLeaf(c.path, c.a), b: maskDiffLeaf(c.path, c.b) }))
  assert.equal(d.length, 1)
  assert.equal(d[0].path, 'x.api.key')
  assert.ok(!String(d[0].a).includes('hunter2hunter'), `old value leaked: ${d[0].a}`)
  assert.ok(!String(d[0].b).includes('hunter3hunter'), `new value leaked: ${d[0].b}`)
})

test('sanitizeTerminal strips CSI, OSC, and bare ESC; leaves normal strings; passes non-strings', () => {
  assert.equal(sanitizeTerminal('\x1b[31mred\x1b[0m'), 'red')
  assert.equal(sanitizeTerminal('\x1b]0;title\x07'), '')
  assert.equal(sanitizeTerminal('\x1b]0;title\x1b\\'), '')
  assert.equal(sanitizeTerminal('\x1bX'), 'X')
  assert.equal(sanitizeTerminal('hello'), 'hello')
  assert.equal(sanitizeTerminal('https://api.example.com'), 'https://api.example.com')
  assert.equal(sanitizeTerminal(''), '')
  assert.equal(sanitizeTerminal(1), 1)
  assert.equal(sanitizeTerminal(null), null)
  assert.equal(sanitizeTerminal(undefined), undefined)
  const obj = { a: 1 }
  assert.equal(sanitizeTerminal(obj), obj)
  assert.ok(!sanitizeTerminal('\x1b[31mred\x1b[0m').includes('\x1b'))
})

test('maskSettings masks and sanitizes a secret that contains terminal escapes', () => {
  const evil = 'sk-abcdefghijklmnop\x1b[31mWORM\x1b[0m'
  const m = maskSettings({ apiKey: evil })
  assert.notEqual(m.apiKey, evil)
  assert.ok(!String(m.apiKey).includes('\x1b'), `escape survived masking: ${JSON.stringify(m.apiKey)}`)
  assert.ok(!String(m.apiKey).includes('WORM'), `secret payload leaked: ${m.apiKey}`)
  assert.match(m.apiKey, /\(len=\d+\)$/)
  // non-secret ordinary string with CSI is stripped, not left raw
  const plain = maskSettings({ model: '\x1b[31msonnet\x1b[0m' })
  assert.equal(plain.model, 'sonnet')
  assert.ok(!String(plain.model).includes('\x1b'))
})

test('__proto__ is a real profile key, not a prototype setter', () => {
  // A profile with a __proto__ key must hash differently from {} — the old
  // plain-object canonicalizer swallowed the key and both collapsed to {}.
  const withProto = JSON.parse('{"__proto__":{"x":1}}')
  assert.notEqual(contentHash(withProto), contentHash({}))
  assert.notEqual(canonicalize(withProto), canonicalize({}))
  assert.ok(!deepEqual(withProto, {}))
  // and masking keeps it as a visible own key rather than dropping it
  const m = maskSettings(withProto)
  assert.ok(Object.prototype.hasOwnProperty.call(m, '__proto__'))
})
