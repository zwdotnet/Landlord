const test = require('node:test')
const assert = require('node:assert/strict')
const path = require('node:path')
const root = path.resolve(__dirname, '../../miniprogram')
function environment() {
  for (const key of Object.keys(require.cache)) if (key.startsWith(root + path.sep)) delete require.cache[key]
  const storage = new Map(), requests = [], downloads = [], removed = [], opened = []
  global.wx = {
    env: { USER_DATA_PATH: '/test-user-files' },
    getStorageSync: key => storage.get(key), setStorageSync: (key, value) => storage.set(key, value), removeStorageSync: key => storage.delete(key),
    request: options => requests.push(options), downloadFile: options => downloads.push(options),
    getFileSystemManager: () => ({ unlinkSync: name => removed.push(name), accessSync() { throw Error('absent') },
      readFile: options => options.success({ data: new TextEncoder().encode('%PDF-1.7\n').buffer }) }),
    openDocument: options => { opened.push(options); options.success({}) },
  }
  return { storage, requests, downloads, removed, opened,
    session: require(root + '/lib/session'), request: require(root + '/lib/request').request,
    files: require(root + '/lib/files') }
}
function identity(id = 1) { return { token: 'synthetic-token-' + id, user: { id, username: 'tenant-' + id, role: 'tenant', api_key: 'must-not-persist' } } }
test('stores only tenant identity and token, never password or API key', () => {
  const e = environment(); e.session.start(identity())
  assert.deepEqual(e.session.current(), { token: identity().token, user: { id: 1, username: 'tenant-1', role: 'tenant' } })
  assert.throws(() => e.session.start({ token: 'owner', user: { id: 2, role: 'admin' } }), /租客/)
})
test('expiry clears current identity and rejects protected result', async () => {
  const e = environment(); e.session.start(identity())
  const pending = e.request('/auth/me')
  assert.equal(e.requests[0].header.Authorization, 'Bearer ' + identity().token)
  e.requests[0].success({ statusCode: 401, data: { code: 401, message: 'expired' } })
  await assert.rejects(pending, { status: 401 }); assert.equal(e.session.token(), '')
})
test('late 401 from old login cannot clear a different account', async () => {
  const e = environment(); e.session.start(identity())
  const pending = e.request('/auth/me'); e.session.start(identity(2))
  e.requests[0].success({ statusCode: 401, data: { code: 401 } })
  await assert.rejects(pending, { stale: true }); assert.equal(e.session.token(), identity(2).token)
})
test('late successful data is discarded after logout', async () => {
  const e = environment(); e.session.start(identity())
  const pending = e.request('/rental/portal/profile'); e.session.clear()
  e.requests[0].success({ statusCode: 200, data: { code: 0, data: { private: true } } })
  await assert.rejects(pending, { stale: true })
})
test('public login never sends an existing bearer token', async () => {
  const e = environment(); e.session.start(identity())
  const pending = e.request('/auth/login', { public: true, method: 'POST' })
  assert.equal(e.requests[0].header.Authorization, undefined)
  e.requests[0].success({ statusCode: 200, data: { code: 0, data: {} } }); await pending
})
test('business error inside HTTP 200 is not accepted', async () => {
  const e = environment(); e.session.start(identity())
  const pending = e.request('/rental/portal/bills')
  e.requests[0].success({ statusCode: 200, data: { code: 403, message: 'forbidden' } })
  await assert.rejects(pending, /forbidden/)
})
test('request timeout and malformed envelope produce errors, never empty success', async () => {
  const e = environment(); e.session.start(identity())
  let pending = e.request('/rental/portal/bills'); e.requests[0].fail({ errMsg: 'timeout' })
  await assert.rejects(pending, /连接失败/)
  pending = e.request('/rental/portal/bills'); e.requests[1].success({ statusCode: 200, data: '<html>error</html>' })
  await assert.rejects(pending, /服务暂不可用/)
})
test('download success callback with HTTP 403 never opens a PDF', async () => {
  const e = environment(); e.session.start(identity())
  const pending = e.files.open('/rental/portal/documents/5/file')
  e.downloads[0].success({ statusCode: 403, tempFilePath: 'fake-error.json' })
  await assert.rejects(pending, { status: 403 }); assert.equal(e.opened.length, 0)
  assert.ok(e.removed.includes('fake-error.json'))
})
test('HTTP 200 JSON disguised as a PDF is rejected and removed', async () => {
  const e = environment(); e.session.start(identity())
  wx.getFileSystemManager = () => ({ unlinkSync: name => e.removed.push(name), readFile: options => options.success({ data: new TextEncoder().encode('{"code":401}').buffer }) })
  const pending = e.files.open('/rental/portal/documents/5/file')
  e.downloads[0].success({ statusCode: 200, tempFilePath: 'fake-pdf' })
  await assert.rejects(pending, /不是有效文件/); assert.equal(e.opened.length, 0); assert.ok(e.removed.includes('fake-pdf'))
})
test('download completed after account switch is deleted without preview', async () => {
  const e = environment(); e.session.start(identity())
  const pending = e.files.open('/rental/portal/contracts/5/file')
  e.session.start(identity(2)); e.downloads[0].success({ statusCode: 200, tempFilePath: 'old-account.pdf' })
  await assert.rejects(pending, { stale: true }); assert.equal(e.opened.length, 0); assert.ok(e.removed.includes('old-account.pdf'))
})
test('valid PDF is opened locally with bearer header, and tracked for cleanup', async () => {
  const e = environment(); e.session.start(identity())
  const pending = e.files.open('/rental/portal/documents/5/file')
  assert.equal(e.downloads[0].header.Authorization, 'Bearer ' + identity().token)
  assert.ok(!e.downloads[0].url.includes('token='))
  e.downloads[0].success({ statusCode: 200, tempFilePath: 'test.pdf' }); await pending
  assert.equal(e.opened[0].fileType, 'pdf'); e.files.clean(); assert.ok(e.removed.includes('test.pdf'))
})
test('file route rejects arbitrary URLs and cross-module paths', async () => {
  const e = environment(); e.session.start(identity())
  await assert.rejects(e.files.open('https://other.example/doc.pdf'), /无效/)
  await assert.rejects(e.files.open('/rental/documents/1/file'), /无效/)
  assert.equal(e.downloads.length, 0)
})
test('payable respects carry-in, zero due, and negative overpayment', () => {
  const fmt = require(root + '/lib/format')
  assert.equal(fmt.bill({ total_amount: 100, carry_in: 20, payable: 120, paid_amount: 50 }).dueText, '70.00')
  assert.equal(fmt.bill({ total_amount: 100, carry_in: -100, payable: 0, paid_amount: 0 }).payableText, '0.00')
  assert.equal(fmt.bill({ total_amount: 100, payable: 100, paid_amount: 120 }).dueText, '0.00')
  assert.equal(fmt.bill({ total_amount: 100, carry_in: 20, paid_amount: 50 }).payableText, '120.00')
})
test('payment distinguishes reversal from voided original and uses Hong Kong time', () => {
  const fmt = require(root + '/lib/format')
  assert.equal(fmt.payment({ amount: 100, voided_at: 'today' }).stateText, '已冲销')
  assert.equal(fmt.payment({ amount: -100, reversal_of_id: 1 }).kindText, '冲销')
  assert.equal(fmt.time('2026-11-12T01:20:00Z'), '2026-11-12 09:20')
})
test('unbound tenant is routed to pending before protected loader runs', async () => {
  const e = environment(); e.session.start(identity())
  let definition, loaded = false, destination = ''
  global.Page = options => { definition = options }
  global.getCurrentPages = () => []
  wx.reLaunch = options => { destination = options.url }
  require(root + '/lib/page').tenantPage({ initial: () => ({ secret: null }), load: async () => { loaded = true; return { secret: true } } })
  const page = Object.assign({}, definition, { setData(data) { Object.assign(this.data, data) } })
  page.onLoad({}); const pending = page.refresh()
  e.requests[0].success({ statusCode: 200, data: { code: 0, data: { role: 'tenant' } } }); await new Promise(setImmediate)
  e.requests[1].success({ statusCode: 200, data: { code: 0, data: { bound: false } } }); await pending
  assert.equal(loaded, false); assert.equal(destination, '/pages/pending/index'); assert.equal(page.data.secret, null)
})
test('anonymous page redirects once without fetching protected data', async () => {
  const e = environment()
  let definition, redirects = 0, loaded = false
  global.Page = options => { definition = options }
  global.getCurrentPages = () => []
  wx.reLaunch = () => { redirects++ }
  require(root + '/lib/page').tenantPage({ initial: () => ({}), load: async () => { loaded = true; return {} } })
  const page = Object.assign({}, definition, { setData(data) { Object.assign(this.data, data) } })
  page.onLoad({}); await page.refresh()
  assert.equal(redirects, 1); assert.equal(loaded, false); assert.equal(e.requests.length, 0)
})
test('late page loader cannot repopulate data after hide or account switch', async () => {
  const e = environment(); e.session.start(identity())
  let definition, resolveLoader
  global.Page = options => { definition = options }
  require(root + '/lib/page').tenantPage({ initial: () => ({ secret: null }), load: () => new Promise(resolve => { resolveLoader = resolve }) })
  const page = Object.assign({}, definition, { setData(data) { Object.assign(this.data, data) } })
  page.onLoad({}); const pending = page.refresh()
  e.requests[0].success({ statusCode: 200, data: { code: 0, data: { role: 'tenant' } } }); await new Promise(setImmediate)
  e.requests[1].success({ statusCode: 200, data: { code: 0, data: { bound: true } } }); await new Promise(setImmediate)
  page.onHide(); e.session.start(identity(2)); resolveLoader({ secret: 'old account data' }); await pending
  assert.equal(page.data.secret, null)
})

function loginPage() {
  const e = environment()
  let definition
  global.Page = options => { definition = options }
  require(root + '/pages/login/index')
  const page = Object.assign({}, definition, { data: { ...definition.data }, setData(data) { Object.assign(this.data, data) } })
  page.onLoad()
  return { ...e, page }
}

test('WeChat first login asks phone, retries with a fresh code, and establishes tenant session', async () => {
  const e = loginPage(); let codes = 0, switched = ''
  wx.login = options => options.success({ code: 'fresh-code-' + (++codes) })
  wx.switchTab = options => { switched = options.url }
  let pending = e.page.submit(); await new Promise(setImmediate)
  assert.ok(e.requests[0].url.endsWith('/auth/wechat/login'))
  assert.equal(e.requests[0].data.password, undefined)
  e.requests[0].success({ statusCode: 200, data: { code: 0, data: { phone_required: true } } }); await pending
  assert.equal(e.page.data.phoneRequired, true); assert.equal(e.session.token(), '')
  e.page.input({ detail: { value: '138' + '0000' + '9001' } })
  pending = e.page.submit(); await new Promise(setImmediate)
  assert.equal(e.requests[1].data.code, 'fresh-code-2')
  assert.equal(e.requests[1].data.phone, '138' + '0000' + '9001')
  e.requests[1].success({ statusCode: 200, data: { code: 0, data: identity() } }); await pending
  assert.equal(switched, '/pages/home/index'); assert.equal(e.session.token(), identity().token)
  assert.equal(e.page.data.phone, '')
})

test('cancelled or hidden WeChat login never creates a stale session', async () => {
  const e = loginPage(); let callback
  wx.login = options => { callback = options }
  let pending = e.page.submit(); callback.fail({}); await pending
  assert.match(e.page.data.error, /微信登录未完成/); assert.equal(e.requests.length, 0)
  pending = e.page.submit(); e.page.onHide(); callback.success({ code: 'old-code' }); await pending
  assert.equal(e.requests.length, 0); assert.equal(e.session.token(), '')
  pending = e.page.submit(); callback.success({ code: 'second-code' }); await new Promise(setImmediate)
  e.page.onHide()
  e.requests[0].success({ statusCode: 200, data: { code: 0, data: identity() } }); await pending
  assert.equal(e.session.token(), '')
})
