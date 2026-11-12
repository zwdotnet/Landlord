// Exercise the actual client modules against the fresh SQLite fixture.
// wx transport/lifecycle are adapted for Node; this is not a native-device test.
const fs = require('node:fs'), path = require('node:path'), assert = require('node:assert/strict')
const root = path.resolve(__dirname, '../..'), client = path.join(root, 'miniprogram')
const out = fs.readFileSync(path.join(root, '.runtime/m5-20261112/fixture-path.txt'), 'utf8').trim()
const state = JSON.parse(fs.readFileSync(path.join(out, 'state.json'), 'utf8'))
const storage = new Map(), checks = []
let definition, destination, opened = 0
global.getCurrentPages = () => []
global.Page = value => { definition = value }
global.wx = {
  env: { USER_DATA_PATH: out },
  getStorageSync: key => storage.get(key), setStorageSync: (key, value) => storage.set(key, value), removeStorageSync: key => storage.delete(key),
  reLaunch: options => { destination = options.url }, switchTab: options => { destination = options.url },
  request(options) {
    const url = new URL(options.url)
    if (options.method === 'GET' && options.data) Object.entries(options.data).forEach(([k, v]) => url.searchParams.set(k, v))
    fetch(url, { method: options.method, headers: options.header, body: options.method === 'POST' ? JSON.stringify(options.data || {}) : undefined })
      .then(async res => options.success({ statusCode: res.status, data: await res.json() })).catch(options.fail)
  },
  downloadFile(options) {
    fetch(options.url, { headers: options.header }).then(async res => {
      const file = options.filePath
      fs.writeFileSync(file, Buffer.from(await res.arrayBuffer()))
      options.success({ statusCode: res.status, filePath: file })
    }).catch(options.fail)
  },
  getFileSystemManager: () => ({ unlinkSync: name => fs.unlinkSync(name), accessSync: name => fs.accessSync(name),
    readFile(options) { const b = fs.readFileSync(options.filePath); options.success({ data: b.buffer.slice(b.byteOffset, b.byteOffset + b.byteLength) }) } }),
  openDocument(options) { assert.ok(fs.readFileSync(options.filePath).subarray(0, 5).equals(Buffer.from('%PDF-'))); opened++; options.success({}) },
  showToast() {}, stopPullDownRefresh() {},
}
const session = require(client + '/lib/session'), files = require(client + '/lib/files')
session.subscribe(() => files.clean())
function page(name, query = {}) {
  const module = client + '/pages/' + name + '/index.js'
  delete require.cache[require.resolve(module)]; require(module)
  const instance = Object.assign({}, definition, { data: structuredClone(definition.data), setData(data) { Object.assign(this.data, data) } })
  if (instance.onLoad) instance.onLoad(query)
  return instance
}
const { fixtureLogin } = require('./fixture-login.cjs')
async function login(username, password = state.password) {
  session.start(await fixtureLogin(state,username,password))
}
async function check(name, fn) { await fn(); checks.push({ name, pass: true }); console.log('PASS ' + name) }
;(async () => {
  await check('fixture API rejects incorrect credentials over real HTTP', async () => {
    await assert.rejects(login('mini-renter', 'bad-credentials')); assert.equal(session.token(), '')
  })
  await check('unbound login routes to waiting without fetching private profile', async () => {
    await login('mini-pending'); const home = page('home'); await home.refresh(); assert.equal(destination, '/pages/pending/index'); assert.equal(home.data.profile, null)
    const pending = page('pending'); await pending.refresh(); assert.equal(pending.data.username, 'mini-pending'); pending.logout()
  })
  await check('bound tenant reads profile and latest bill through client guard', async () => {
    await login('mini-renter'); const p = page('home'); await p.refresh(); assert.equal(p.data.error, ''); assert.equal(p.data.profile.name, '测试租客'); assert.equal(p.data.latest.id, state.bill_id)
  })
  await check('bill pages, receipt IDs and reversal display match SQLite response', async () => {
    const bills = page('bills'); await bills.refresh(); assert.equal(bills.data.items.length, 10); assert.equal(bills.data.total, 12)
    bills.targetPage = 2; await bills.refresh(); assert.equal(bills.data.items.length, 2)
    const detail = page('bill', { id: String(state.bill_id) }); await detail.refresh()
    assert.equal(detail.data.bill.paidText, '600.00'); assert.equal(detail.data.payments.length, 3)
    assert.ok(detail.data.payments.some(p => p.kindText === '冲销')); assert.ok(detail.data.payments.some(p => p.stateText === '已冲销'))
  })
  await check('contracts and payment screen load actual protected documents', async () => {
    const contracts = page('contracts'); await contracts.refresh(); assert.equal(contracts.data.agreements[0].id, state.agreement_id)
    const payments = page('payments'); await payments.refresh(); assert.equal(payments.data.items.length, 3)
    const profile = page('profile'); await profile.refresh(); assert.equal(profile.data.username, 'mini-renter')
  })
  await check('client downloads actual PDF and deletes it at logout', async () => {
    await files.open('/rental/portal/documents/' + state.agreement_id + '/file'); assert.equal(opened, 1)
    const key = ['landlord', 'tenant', 'files', 'v1'].join('.'), paths = [...storage.get(key)]
    page('profile').logout(); paths.forEach(name => assert.equal(fs.existsSync(name), false)); assert.equal(session.token(), '')
  })
  await check('another tenant has empty lists and cannot read first tenant bill or PDF', async () => {
    await login('mini-second'); const home = page('home'); await home.refresh(); assert.equal(home.data.profile.name, '另一位租客'); assert.equal(home.data.latest, null)
    const bills = page('bills'); await bills.refresh(); assert.equal(bills.data.total, 0)
    const detail = page('bill', { id: String(state.bill_id) }); await detail.refresh(); assert.ok(detail.data.error); assert.equal(detail.data.bill, null)
    await assert.rejects(files.open('/rental/portal/documents/' + state.agreement_id + '/file'), { status: 404 })
  })
  await check('server rejects revoked token and client removes stale identity', async () => {
    const key = ['landlord', 'tenant', 'session', 'v1'].join('.'); storage.set(key, { token: 'invalid', user: { role: 'tenant' } })
    const p = page('home'); await p.refresh(); assert.equal(session.token(), ''); assert.equal(destination, '/pages/login/index'); assert.equal(p.data.profile, null)
  })

})().catch(e => { process.exitCode = 1; console.error(e.stack) }).finally(() => {
  files.clean(); session.clear()
  fs.writeFileSync(path.join(out, 'http-results.json'), JSON.stringify({ observed_at: new Date().toISOString(), checks, pass: !process.exitCode,
    scope: 'Actual client modules + real HTTP/SQLite; Node wx adapter, no native preview acceptance' }, null, 2))
})
