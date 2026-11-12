// Run after serve-fixture.py and `cli auto --auto-port 9420`. Uses only synthetic local identities.
const fs = require('node:fs')
const path = require('node:path')
const assert = require('node:assert/strict')
const root = path.resolve(__dirname, '../..')
const automator = require(path.join(root, '.runtime/m5-tools/node_modules/miniprogram-automator'))
const out = fs.readFileSync(path.join(root, '.runtime/m5-20261112/fixture-path.txt'), 'utf8').trim()
const state = JSON.parse(fs.readFileSync(path.join(out, 'state.json'), 'utf8'))
const checks = [], errors = []
let mp
async function check(name, run) { await run(); checks.push({ name, pass: true }); console.log('PASS ' + name) }
async function until(fn, message) {
  const end = Date.now() + 25000
  while (Date.now() < end) { if (await fn()) return; await new Promise(r => setTimeout(r, 180)) }
  throw Error('Timed out: ' + message)
}
async function ready(route) {
  let page
  await until(async () => {
    const native = await mp.evaluate(() => { const pages = getCurrentPages(); const p = pages[pages.length - 1]; return p && { route: p.route, loading: p.data.loading, error: p.data.error } })
    if (!native || native.route !== route || native.loading) return false
    try { page = await mp.currentPage() } catch (_) { return false }
    if (!page || page.path !== route) return false
    const data = await page.data()
    if (data.error) throw Error(route + ': ' + data.error)
    return !data.loading
  }, route)
  await new Promise(r => setTimeout(r, 400))
  return page
}
async function invoke(method) { return mp.evaluate(name => { const pages = getCurrentPages(); return pages[pages.length - 1][name]() }, method) }
async function navigate(method, url) {
  const target = url.split('?')[0].replace(/^\//, '')
  const current = await mp.evaluate(() => { const pages = getCurrentPages(); return pages.length ? pages[pages.length - 1].route : '' })
  if (current === target && !url.includes('?')) return ready(target)
  await mp.evaluate((name, target) => { wx[name]({ url: target }); return true }, method, url)
  return ready(target)
}
const { nativeFixtureLogin } = require('./fixture-login.cjs')
async function login(username, password = state.password) {
  await nativeFixtureLogin(mp, state, username, password)
}
async function shot(name) { await mp.screenshot({ path: path.join(out, name + '.png') }) }
;(async () => {
  mp = await automator.connect({ wsEndpoint: 'ws://127.0.0.1:9420' })
  mp.on('exception', e => errors.push(String(e.message || e)))
  await check('native login page exposes WeChat entry without password fields', async () => {
    const page = await navigate('reLaunch', '/pages/login/index')
    assert.equal((await page.$$('input')).length, 0)
    await shot('login')
  })
  await check('fixture API rejects incorrect credentials', async () => {
    await assert.rejects(login('mini-renter', 'incorrect-' + Date.now()))
  })
  await check('unbound tenant enters pending page', async () => {
    await login('mini-pending'); const page = await ready('pages/pending/index')
    assert.equal((await page.data()).username, 'mini-pending'); await shot('pending')
    await invoke('logout'); await ready('pages/login/index')
  })
  await check('tenant login loads actual SQLite profile and latest bill', async () => {
    await login('mini-renter'); const page = await ready('pages/home/index')
    const data = await page.data(); assert.equal(data.profile.name, '测试租客'); assert.equal(data.latest.id, state.bill_id)
    assert.equal(data.latest.paidText, '600.00'); await shot('home')
  })
  await check('native bill pagination and navigation use real protected API', async () => {
    let page = await navigate('switchTab', '/pages/bills/index')
    assert.equal((await page.data()).items.length, 10); assert.equal((await page.data()).total, 12)
    await invoke('next'); await until(async () => { const d = await page.data(); return !d.loading && d.page === 2 }, 'bill page 2')
    assert.equal((await page.data()).items.length, 2)
    await invoke('previous'); await until(async () => { const d = await page.data(); return !d.loading && d.page === 1 }, 'bill page 1')
    await shot('bills')
    await (await page.$('.surface')).tap(); page = await ready('pages/bill/index')
    const data = await page.data(); assert.equal(data.bill.id, state.bill_id); assert.equal(data.payments.length, 3)
    assert.ok(data.payments.some(p => p.kindText === '冲销')); assert.ok(data.payments.some(p => p.stateText === '已冲销'))
    await shot('bill-detail')
  })
  await check('native contracts page loads archived original', async () => {
    const page = await navigate('switchTab', '/pages/contracts/index')
    assert.ok((await page.data()).agreements.some(d => d.id === state.agreement_id)); await shot('contracts')
  })
  await check('actual wx.downloadFile sends bearer and receives valid archived PDF', async () => {
    // Observe the native preview call without fabricating its success response.
    await mp.evaluate(() => {
      const original = wx.openDocument
      wx.__landlordPreviewOriginal = original
      wx.__landlordPreviewResult = null
      wx.openDocument = function(options) {
        return original(Object.assign({}, options, {
          success(result) { wx.__landlordPreviewResult = { success: true }; if (options.success) options.success(result) },
          fail(result) { wx.__landlordPreviewResult = { success: false, message: result.errMsg }; if (options.fail) options.fail(result) },
        }))
      }
    })
    try {
      const page = await mp.currentPage(); await (await page.$('button[data-path]')).tap()
      await until(async () => !!await mp.evaluate(() => wx.__landlordPreviewResult), 'native PDF preview callback')
      const result = await mp.evaluate(() => wx.__landlordPreviewResult)
      assert.ok(result.success, result.message)
      const tracked = await mp.evaluate(() => wx.getStorageSync(['landlord', 'tenant', 'files', 'v1'].join('.')) || [])
      assert.ok(tracked.length > 0)
    } finally {
      await mp.evaluate(() => { if (wx.__landlordPreviewOriginal) wx.openDocument = wx.__landlordPreviewOriginal; delete wx.__landlordPreviewOriginal; delete wx.__landlordPreviewResult })
    }
  })
  await check('payment screen shows original and reversal records', async () => {
    const page = await navigate('reLaunch', '/pages/payments/index')
    assert.equal((await page.data()).items.length, 3); await shot('payments')
  })
  await check('logout removes identity and temporary downloads', async () => {
    const page = await navigate('switchTab', '/pages/profile/index'); await shot('profile')
    await invoke('logout'); await ready('pages/login/index')
    const info = await mp.evaluate(() => ({ session: !!wx.getStorageSync(['landlord', 'tenant', 'session', 'v1'].join('.')), files: wx.getStorageSync(['landlord', 'tenant', 'files', 'v1'].join('.')) || [] }))
    assert.equal(info.session, false); assert.equal(info.files.length, 0)
  })
  await check('switching tenant cannot reveal previous tenant profile or bills', async () => {
    await login('mini-second'); let page = await ready('pages/home/index')
    assert.equal((await page.data()).profile.name, '另一位租客'); assert.equal((await page.data()).latest, null)
    page = await navigate('switchTab', '/pages/bills/index'); assert.equal((await page.data()).items.length, 0)
    await shot('empty-bills')
    await mp.evaluate(url => { wx.navigateTo({ url }); return true }, '/pages/bill/index?id=' + state.bill_id)
    await until(async () => { const p = await mp.currentPage(); if (p && p.path === 'pages/bill/index') { page = p; return true } return false }, 'forbidden detail route')
    await until(async () => !!(await page.data()).error, 'cross-account detail rejection')
    assert.equal((await page.data()).bill, null)
  })
  await check('expired token returns to login and clears protected page', async () => {
    await mp.evaluate(() => { const key = ['landlord', 'tenant', 'session', 'v1'].join('.'); const s = wx.getStorageSync(key); s.token = 'expired'; wx.setStorageSync(key, s) })
    await mp.evaluate(() => { wx.reLaunch({ url: '/pages/home/index' }); return true }); await ready('pages/login/index')
    const present = await mp.evaluate(() => !!wx.getStorageSync(['landlord', 'tenant', 'session', 'v1'].join('.')))
    assert.equal(present, false)
  })
  await check('native login has no registration controls', async () => {
    const page = await navigate('reLaunch', '/pages/login/index')
    assert.equal((await page.data()).register, undefined)
    assert.equal((await page.$$('input[data-field="password"]')).length, 0)
  })
  assert.equal(errors.length, 0, 'native runtime exceptions')
})().catch(error => {
  process.exitCode = 1
  console.error(error.stack)
  errors.push(error.message)
}).finally(async () => {
  const result = { observed_at: new Date().toISOString(), checks, errors, scope: 'Native WeChat simulator + fresh synthetic SQLite; not physical device or production acceptance' }
  fs.writeFileSync(path.join(out, 'native-results-' + Date.now() + '.json'), JSON.stringify(result, null, 2))
  fs.writeFileSync(path.join(out, 'native-results.json'), JSON.stringify(result, null, 2))
  if (mp) { try { await mp.evaluate(() => { wx.reLaunch({ url: '/pages/login/index' }); return true }) } catch (_) {} mp.disconnect() }
})
