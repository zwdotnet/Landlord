const test = require('node:test'), assert = require('node:assert/strict'), path = require('node:path')
const root = path.resolve(__dirname, '../../miniprogram'), tick = () => new Promise(setImmediate)
const bill = (paid = 0) => ({ id: 70, ledger_id: 9, room_no: 'P-01', period: '2030-01', total_amount: 1000, payable: 1000, carry_in: 0, paid_amount: paid, updated_at: '2030-01-02T00:00:00Z' })
const detail = (paid = 0) => ({ bill: bill(paid), payments: [] })
const payment = () => ({ id: 21, bill_id: 70, amount: 500, note: '线下转账', receipt_id: 51, paid_at: '2030-01-02T00:00:00Z' })
function env(storage = new Map()) {
  for (const k of Object.keys(require.cache)) if (k.startsWith(root + path.sep)) delete require.cache[k]
  const requests = [], destinations = [], downloads = [], removed = [], opened = []
  global.wx = { env: { USER_DATA_PATH: '/test-files' }, getStorageSync: k => storage.get(k), setStorageSync: (k, v) => storage.set(k, JSON.parse(JSON.stringify(v))), removeStorageSync: k => storage.delete(k), request: o => requests.push(o), downloadFile: o => downloads.push(o), reLaunch: o => destinations.push(o.url), navigateTo: o => destinations.push(o.url), switchTab: o => destinations.push(o.url), stopPullDownRefresh() {}, showToast() {}, getFileSystemManager: () => ({ unlinkSync: f => removed.push(f), accessSync() { throw Error('absent') }, readFile: o => o.success({ data: new TextEncoder().encode('%PDF-1.7\n').buffer }) }), openDocument: o => { opened.push(o); o.success({}) } }
  global.getCurrentPages = () => []
  const session = require(root + '/lib/session')
  if (!session.token()) session.start({ token: 'synthetic-payment-session', user: { id: 5, username: 'owner', role: 'user' } })
  const landlord = require(root + '/lib/landlord'), intent = require(root + '/lib/payment-intent'), files = require(root + '/lib/files')
  let def; global.Page = v => { def = v }; require(root + '/pages/landlord/pay/index')
  const p = { ...def, data: { ...def.data }, setData(v) { Object.assign(this.data, v) } }; p.onLoad({ id: '70' })
  return { p, session, landlord, intent, files, storage, requests, destinations, downloads, removed, opened }
}
function respond(e, n, data, status = 200) { e.requests[n].success({ statusCode: status, data: { code: status === 200 ? 0 : status, data, message: 'fixture failure' } }) }
async function ready(e, tier = 'edit', status = null) {
  const p = e.p.refresh()
  respond(e, 0, { role: 'user', username: 'owner' }); await tick()
  respond(e, 1, { status: 1 }); await tick()
  respond(e, 2, [{ id: 9, name: 'payments', status: 'active', perms: { bills: tier } }]); await tick()
  respond(e, 3, detail()); await tick()
  if (status) { respond(e, 4, status); await tick() }
  await p
}
async function review(e, amount = '500', paid = 0) {
  for (const [field, value] of Object.entries({ amount, note: '线下转账' })) e.p.input({ currentTarget: { dataset: { field } }, detail: { value } })
  const p = e.p.reviewPayment(); respond(e, e.requests.length - 1, detail(paid)); await p
}
test('payment validates decimal money and updates overpayment warning from server balance', async () => {
  const e = env(); await ready(e)
  for (const value of ['', '0', '-1', '0.001', '1e3', '0x10', 'NaN', 'Infinity', '100000001']) assert.throws(() => e.intent.amount(value))
  await review(e, '500', 600); assert.equal(e.p.data.overpay, true); assert.equal(e.p.data.reviewedAmount, '500.00')
  e.p.input({ currentTarget: { dataset: { field: 'amount' } }, detail: { value: '501' } }); assert.equal(e.p.data.reviewed, false)
})
test('submission persists original intent before POST and blocks duplicate tap or navigation', async () => {
  const e = env(); await ready(e); await review(e)
  const p = e.p.submit(), saved = e.intent.load(); await e.p.submit(); e.p.goHome()
  assert.equal(e.requests.length, 6); assert.equal(e.destinations.length, 0)
  assert.equal(e.requests[5].data.request_key, saved.payload.request_key); assert.deepEqual(e.requests[5].data.expected, e.intent.expected(bill()))
  assert.ok(e.requests[5].url.endsWith('/70/pay?ledger_id=9')); assert.equal(saved.payload.amount, 500)
  respond(e, 5, bill(500)); await tick(); respond(e, 6, { found: true, bill: bill(500), payment: payment() }); await p
  assert.equal(e.intent.load(), null); assert.equal(e.p.data.finished, true); assert.equal(e.p.data.payments[0].receipt_id, 51)
})
test('lost response checks original request without issuing a second POST', async () => {
  const e = env(); await ready(e); await review(e)
  let p = e.p.submit(); e.requests[5].fail({}); await p
  assert.equal(e.p.data.pending, true); assert.equal(e.p.data.retryReady, false); await e.p.retry(); assert.equal(e.requests.length, 6)
  const key = e.intent.load().payload.request_key; p = e.p.checkPayment(); assert.equal(e.requests[6].data.request_key, key)
  respond(e, 6, { found: true, bill: bill(500), payment: payment() }); await p
  assert.equal(e.intent.load(), null); assert.equal(e.p.data.finished, true)
})
test('unsent or rejected request retries exact payload with the same key after a check', async () => {
  const e = env(); await ready(e); await review(e)
  let p = e.p.submit(); const original = { ...e.requests[5].data }; respond(e, 5, null, 409); await p
  p = e.p.checkPayment(); respond(e, 6, { found: false, bill: bill(200) }); await p
  assert.equal(e.p.data.retryReady, true); e.p.input({ currentTarget: { dataset: { field: 'amount' } }, detail: { value: '900' } }); assert.equal(e.p.data.amount, '500')
  p = e.p.retry(); assert.equal(e.requests[7].data.request_key, original.request_key); assert.equal(e.requests[7].data.amount, 500); assert.equal(e.requests[7].data.expected.paid_amount, 200)
  respond(e, 7, bill(700)); await tick(); respond(e, 8, { found: true, bill: bill(700), payment: payment() }); await p
})
test('failed readback retains intent and blocks unsafe retries', async () => {
  const e = env(); await ready(e); await review(e)
  let p = e.p.submit(); respond(e, 5, bill(500)); await tick(); e.requests[6].fail({}); await p
  assert.ok(e.intent.load()); assert.equal(e.p.data.retryReady, false)
  p = e.p.checkPayment(); e.requests[7].fail({}); await p; await e.p.retry(); assert.equal(e.requests.length, 8)
})
test('hide clears rendered private data but retains unconfirmed intent for restart', async () => {
  const e = env(); await ready(e); await review(e)
  const p = e.p.submit(); const key = e.intent.load().payload.request_key; e.p.onHide(); respond(e, 5, bill(500)); await p
  assert.equal(e.p.data.bill, null); assert.equal(e.p.data.amount, ''); assert.equal(e.intent.load().payload.request_key, key)
  const restarted = env(e.storage); await ready(restarted, 'edit', { found: true, bill: bill(500), payment: payment() })
  assert.equal(restarted.requests[4].data.request_key, key); assert.equal(restarted.intent.load(), null); assert.equal(restarted.p.data.finished, true)
})
test('restart of an unsent payment enables only an explicit original retry', async () => {
  const e = env(); await ready(e); await review(e)
  const p = e.p.submit(); e.requests[5].fail({}); await p
  const f = env(e.storage); await ready(f, 'edit', { found: false, bill: bill() })
  assert.equal(f.requests.length, 5); assert.equal(f.p.data.retryReady, true); assert.equal(f.p.data.amount, '500')
})
test('another bill intent blocks new registration and logout clears the journal', async () => {
  const e = env(); e.intent.save(9, 80, 500, '待核对'); await ready(e)
  assert.equal(e.p.data.blocked, true); await e.p.reviewPayment(); assert.equal(e.requests.length, 4)
  e.session.clear(); assert.equal(e.intent.load(), null); assert.equal(e.storage.size, 0)
})
test('storage failure prevents sending payment and successful intent has no names or credentials', async () => {
  const e = env(); await ready(e); await review(e)
  wx.setStorageSync = () => { throw Error('quota') }; await e.p.submit(); assert.equal(e.requests.length, 5)
  const f = env(); const saved = f.intent.save(9, 70, 500, '线下转账'); assert.deepEqual(Object.keys(saved).sort(), ['billID', 'ledgerID', 'payload', 'userID', 'version'])
})
test('readonly can recover a pending payment but cannot create or retry one', async () => {
  const e = env(); e.intent.save(9, 70, 500, '线下转账'); await ready(e, 'view', { found: false, bill: bill() })
  await e.p.retry(); await e.p.submit(); assert.equal(e.requests.length, 5); assert.equal(e.p.data.canEdit, false)
})
test('revocation preserves unresolved intent and disables editing', async () => {
  const e = env(); await ready(e); await review(e)
  const p = e.p.submit(); respond(e, 5, null, 403); await p
  assert.equal(e.p.data.canEdit, false); assert.ok(e.intent.load()); assert.equal(e.p.data.retryReady, false)
})
test('current401 clears intent and returns login even when session subscriber hides page', async () => {
  const e = env(); await ready(e); await review(e); e.session.subscribe(() => e.p.clearPrivate())
  const p = e.p.submit(); respond(e, 5, null, 401); await p
  assert.equal(e.intent.load(), null); assert.ok(e.destinations.includes('/pages/login/index'))
})
test('late old-account401 cannot clear a new account or its pending intent', async () => {
  const e = env(); await ready(e); await review(e)
  const p = e.p.submit(); e.session.start({ token: 'new-session', user: { id: 8, role: 'user' } }); e.p.clearPrivate(); e.intent.save(9, 80, 100, '')
  respond(e, 5, null, 401); await p; assert.equal(e.session.token(), 'new-session'); assert.equal(e.intent.load().billID, 80); assert.equal(e.destinations.length, 0)
})
test('mismatched request receipt is retained for manual review, never silently cleared', async () => {
  const e = env(); await ready(e); await review(e)
  const p = e.p.submit(); respond(e, 5, bill(500)); await tick(); respond(e, 6, { found: true, bill: bill(500), payment: { ...payment(), amount: 999 } }); await p
  assert.ok(e.intent.load()); assert.equal(e.p.data.pending, true); assert.equal(e.p.data.finished, false)
})
test('landlord receipt download uses Bearer and explicit ledger, retains tenant path allowlist', async () => {
  const e = env(); const p = e.files.openReceipt(51, 9, () => true)
  assert.ok(e.downloads[0].url.endsWith('/rental/documents/51/file?ledger_id=9')); assert.ok(e.downloads[0].header.Authorization)
  e.downloads[0].success({ statusCode: 200, filePath: e.downloads[0].filePath }); await p; assert.equal(e.opened.length, 1)
  await assert.rejects(e.files.open('/rental/documents/51/file'), /无效/)
  for (const [id, ledger] of [['../1', 9], [1, 0], [1, '9&x=1']]) await assert.rejects(e.files.openReceipt(id, ledger, () => true), /无效/)
})
test('old ledger download401 cannot clear current session or open private document', async () => {
  const e = env(); let current = true
  const p = e.files.openReceipt(51, 9, () => current); current = false
  const file = e.downloads[0].filePath; e.downloads[0].success({ statusCode: 401, filePath: file })
  await assert.rejects(p, { stale: true }); assert.ok(e.session.token()); assert.equal(e.opened.length, 0); assert.ok(e.removed.includes(file))
})
test('landlord malformed PDF is removed and current download401 returns expired login', async () => {
  const e = env(); wx.getFileSystemManager = () => ({ unlinkSync: f => e.removed.push(f), readFile: o => o.success({ data: new TextEncoder().encode('{"error":true}').buffer }) })
  let p = e.files.openReceipt(51, 9, () => true); e.downloads[0].success({ statusCode: 200, filePath: e.downloads[0].filePath }); await assert.rejects(p, /有效文件/)
  p = e.files.openReceipt(51, 9, () => true); e.downloads[1].success({ statusCode: 401, filePath: e.downloads[1].filePath }); await assert.rejects(p, { status: 401 }); assert.equal(e.session.token(), '')
})
