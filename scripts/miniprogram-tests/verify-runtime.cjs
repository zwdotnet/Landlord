// Native simulator page lifecycle + actual wx network/files. Calls page handlers;
// avoids the old automator SDK's unstable Page IDs during navigation.
const fs = require('node:fs'), path = require('node:path'), assert = require('node:assert/strict')
const root = path.resolve(__dirname, '../..')
const automator = require(root + '/.runtime/m5-tools/node_modules/miniprogram-automator')
const out = fs.readFileSync(root + '/.runtime/m5-20261112/fixture-path.txt', 'utf8').trim()
const state = JSON.parse(fs.readFileSync(out + '/state.json', 'utf8'))
const checks = [], exceptions = []
let mp
async function info() { return mp.evaluate(() => { const a = getCurrentPages(), p = a[a.length - 1]; return p ? { route: p.route, data: p.data } : null }) }
async function until(fn, label) {
  const end = Date.now() + 30000
  while (Date.now() < end) { if (await fn()) return; await new Promise(r => setTimeout(r, 200)) }
  throw Error('Timed out: ' + label)
}
async function ready(route) {
  let current
  await until(async () => { current = await info(); if (!current || current.route !== route || current.data.loading) return false; if (current.data.error) throw Error(current.data.error); return true }, route)
  return current.data
}
async function call(name, ...args) { return mp.evaluate((method, values) => { const a = getCurrentPages(); a[a.length - 1][method](...values); return true }, name, args) }
async function nav(route, method = 'switchTab') {
  await mp.evaluate((url, action) => { wx[action]({ url }); return true }, '/' + route, method)
  return ready(route.split('?')[0])
}
const { nativeFixtureLogin } = require('./fixture-login.cjs')
async function login(username, password = state.password) {
  await nativeFixtureLogin(mp, state, username, password)
}
async function check(name, fn) { await fn(); checks.push({ name, pass: true }); console.log('PASS ' + name) }
;(async () => {
  mp = await automator.connect({ wsEndpoint: 'ws://127.0.0.1:9420' })
  mp.on('exception', e => exceptions.push(e.message || String(e)))
  await check('fixture API rejects invalid account credentials', async () => {
    await assert.rejects(login('mini-renter','wrong-password'))
  })
  await check('native lifecycle routes synthetic unbound tenant to pending', async () => {
    await login('mini-pending'); const d = await ready('pages/pending/index'); assert.equal(d.username, 'mini-pending'); await call('logout'); await ready('pages/login/index')
  })
  await check('native lifecycle loads actual profile and last bill', async () => {
    await login('mini-renter'); const d = await ready('pages/home/index'); assert.equal(d.profile.name, '测试租客'); assert.equal(d.latest.id, state.bill_id)
    await mp.screenshot({ path: path.join(out, 'runtime-home.png') })
  })
  await check('native bills pagination and detail including reversals', async () => {
    const d = await nav('pages/bills/index'); assert.equal(d.items.length, 10)
    await call('next'); await until(async () => { const i = await info(); return !i.data.loading && i.data.page === 2 }, 'page2'); assert.equal((await info()).data.items.length, 2)
    const detail = await nav('pages/bill/index?id=' + state.bill_id, 'navigateTo'); assert.equal(detail.payments.length, 3); assert.equal(detail.bill.paidText, '600.00')
    await mp.screenshot({ path: path.join(out, 'runtime-bill.png') })
  })
  await check('native contracts and payment records', async () => {
    const d = await nav('pages/contracts/index'); assert.equal(d.agreements[0].id, state.agreement_id)
    await mp.screenshot({ path: path.join(out, 'runtime-contracts.png') })
    const payments = await nav('pages/payments/index', 'navigateTo'); assert.equal(payments.items.length, 3)
  })
  await check('real wx download validates PDF before native preview', async () => {
    await nav('pages/contracts/index')
    await mp.evaluate(() => {
      const original = wx.openDocument; wx.__previewOriginal = original; wx.__previewResult = null
      wx.openDocument = function(o) { return original(Object.assign({}, o, { success(r) { wx.__previewResult = { success: true }; o.success(r) }, fail(r) { wx.__previewResult = { success: false }; o.fail(r) } })) }
    })
    try {
      await call('openFile', { currentTarget: { dataset: { path: '/rental/portal/documents/' + state.agreement_id + '/file' } } })
      await until(async () => !!await mp.evaluate(() => wx.__previewResult), 'native preview')
      assert.equal((await mp.evaluate(() => wx.__previewResult)).success, true)
    } finally { await mp.evaluate(() => { wx.openDocument = wx.__previewOriginal; delete wx.__previewOriginal; delete wx.__previewResult }) }
  })
  await check('native logout clears identity and temporary files', async () => {
    await nav('pages/profile/index'); await call('logout'); await ready('pages/login/index')
    const d = await mp.evaluate(() => ({ session: !!wx.getStorageSync(['landlord','tenant','session','v1'].join('.')), files: wx.getStorageSync(['landlord','tenant','files','v1'].join('.')) || [] }))
    assert.equal(d.session, false); assert.equal(d.files.length, 0)
  })
  await check('second native tenant cannot display previous account data', async () => {
    await login('mini-second'); const d = await ready('pages/home/index'); assert.equal(d.profile.name, '另一位租客'); assert.equal(d.latest, null)
    const bills = await nav('pages/bills/index'); assert.equal(bills.total, 0)
    await call('logout'); await ready('pages/login/index')
  })
  assert.equal(exceptions.length, 0)
})().catch(e => { process.exitCode = 1; exceptions.push(e.message); console.error(e.stack) }).finally(async () => {
  fs.writeFileSync(path.join(out, 'runtime-results-' + Date.now() + '.json'), JSON.stringify({ observed_at: new Date().toISOString(), checks, exceptions, pass: !process.exitCode,
    scope: 'Native simulator page handlers/lifecycle + actual wx transport/files; no physical device; not full UI tap automation' }, null, 2))
  if (mp) mp.disconnect()
})
