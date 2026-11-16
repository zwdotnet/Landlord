const test = require('node:test')
const assert = require('node:assert/strict')
const fs = require('node:fs')
const os = require('node:os')
const path = require('node:path')
const testConfig = require('./test-config.cjs')

test('client unit tests load without ignored local configuration after cache resets', t => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'landlord-mini-config-'))
  t.after(() => {
    for (const key of Object.keys(require.cache)) if (key.startsWith(root + path.sep)) delete require.cache[key]
    fs.rmSync(root, { recursive: true, force: true })
  })
  fs.mkdirSync(path.join(root, 'lib'))
  for (const name of ['config.js', 'lib/request.js', 'lib/session.js']) {
    fs.copyFileSync(path.resolve(__dirname, '../../miniprogram', name), path.join(root, name))
  }
  assert.equal(fs.existsSync(path.join(root, 'config.local.js')), false)
  for (let i = 0; i < 2; i++) {
    for (const key of Object.keys(require.cache)) if (key.startsWith(root + path.sep)) delete require.cache[key]
    testConfig(root)
    assert.equal(require(path.join(root, 'lib/request')).url('/auth/me'), 'https://landlord-test.invalid/api/auth/me')
  }
  assert.equal(fs.existsSync(path.join(root, 'config.local.js')), false)
})
