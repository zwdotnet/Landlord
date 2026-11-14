const test = require('node:test')
const assert = require('node:assert/strict')
const path = require('node:path')
const root = path.resolve(__dirname, '../../miniprogram')
const tick = () => new Promise(setImmediate)
function env() {
  for (const key of Object.keys(require.cache)) if (key.startsWith(root + path.sep)) delete require.cache[key]
  const storage = new Map(), requests = [], destinations = []
  global.wx = { getStorageSync: k => storage.get(k), setStorageSync: (k, v) => storage.set(k, v), removeStorageSync: k => storage.delete(k), request: o => requests.push(o), reLaunch: o => destinations.push(o.url), switchTab: o => destinations.push(o.url), navigateTo: o => destinations.push(o.url), stopPullDownRefresh() {} }
  global.getCurrentPages = () => []
  const session = require(root + '/lib/session'), landlord = require(root + '/lib/landlord')
  session.start({ token: 'synthetic-issue-session', user: { id: 5, role: 'user', username: 'owner' } })
  let definition; global.Page = o => { definition = o }; require(root + '/pages/landlord/issue/index')
  const p = Object.assign({}, definition, { data: { ...definition.data }, setData(v) { Object.assign(this.data, v) } }); p.onLoad({})
  return { p, requests, destinations, session, landlord, storage }
}
function respond(e, i, data, status = 200) { e.requests[i].success({ statusCode: status, data: { code: status === 200 ? 0 : status, message: 'fixture denial', data } }) }
const bill = { id: 0, room_no: 'I-01', period: '2030-01', total_amount: 1000, carry_in: 130, payable: 1130 }
const view = () => ({ bill, items: [{ key: 'rent', name: '租金', amount: 1000 }], can_confirm: true, confirmation_token: 'synthetic-preview', exists: false })
async function ready(e, tier = 'edit') {
  const pending = e.p.refresh()
  respond(e, 0, { role: 'user', username: 'owner' }); await tick()
  respond(e, 1, { status: 1 }); await tick()
  respond(e, 2, [{ id: 9, status: 'active', name: 'bills only', perms: { bills: tier } }]); await tick()
  respond(e, 3, { items: [{ id: 1, room_no: 'I-01' }], total: 1 }); await pending
  e.p.setData({ period: '2030-01' })
  const choosing = e.p.chooseRoom({ currentTarget: { dataset: { id: 1 } } })
  respond(e, 4, { tenants: [{ id: 11, name: '同名' }, { id: 12, name: '同名' }] }); await choosing
  e.p.selectTenants({ detail: { value: ['12'] } })
}
async function preview(e, v = view()) { const p = e.p.preview(); respond(e, e.requests.length - 1, v); await p }
test('bill-only candidates distinguish same-name stay IDs and use server totals', async () => {
  const e = env(); await ready(e); await preview(e)
  assert.ok(e.requests[3].url.includes('/bills/issue-rooms?ledger_id=9'))
  assert.equal(e.requests[5].data.tenant_ids, '12'); assert.equal(e.p.data.bill.payableText, '1130.00')
  assert.equal(e.p.data.fees[0].amountText, '1000.00'); assert.equal(e.p.data.canConfirm, true)
  assert.ok(!JSON.stringify(e.p.data).includes(view().confirmation_token))
  assert.equal(e.storage.size, 1) // session storage only
})
test('confirm sends only preview token and prevents duplicate taps and navigation', async () => {
  const e = env(); await ready(e); await preview(e)
  const p = e.p.confirm(); await e.p.confirm(); e.p.goHome(); e.p.changeRoom(); e.p.changePeriod({ detail: { value: '2030-02' } })
  assert.equal(e.requests.length, 7); assert.equal(e.destinations.length, 0); assert.equal(e.p.data.period, '2030-01')
  assert.deepEqual(e.requests[6].data, { confirmation_token: 'synthetic-preview' }); assert.ok(e.requests[6].url.endsWith('?ledger_id=9'))
  respond(e, 6, { created: true, bill: { ...bill, id: 70 } }); await p
  assert.equal(e.p.data.exists, true); assert.equal(e.p.data.canConfirm, false); e.p.goBill(); assert.ok(e.destinations[0].endsWith('id=70'))
})
test('lost confirmation response cannot resend and reconciles original bill', async () => {
  const e = env(); await ready(e); await preview(e)
  const p = e.p.confirm(); e.requests[6].fail({ errMsg: 'network lost' }); await p
  assert.equal(e.p.data.needsCheck, true); await e.p.confirm(); await e.p.preview(); assert.equal(e.requests.length, 7)
  const c = e.p.checkIssued(); respond(e, 7, { ...view(), exists: true, can_confirm: false, bill: { ...bill, id: 80 } }); await c
  assert.equal(e.p.data.needsCheck, false); assert.equal(e.p.data.bill.id, 80); assert.equal(e.p.data.canConfirm, false)
})
test('request not sent requires a fresh preview and explicit confirmation', async () => {
  const e = env(); await ready(e); await preview(e)
  let p = e.p.confirm(); e.requests[6].fail({ errMsg: 'offline' }); await p
  p = e.p.checkIssued(); respond(e, 7, { ...view(), confirmation_token: 'fresh' }); await p
  assert.equal(e.requests.length, 8); assert.equal(e.p.data.canConfirm, true)
  p = e.p.confirm(); assert.equal(e.requests[8].data.confirmation_token, 'fresh'); respond(e, 8, { created: true, bill: { ...bill, id: 1 } }); await p
})
test('failed readback stays blocked until a successful explicit check', async () => {
  const e = env(); await ready(e); await preview(e)
  let p = e.p.confirm(); respond(e, 6, null, 500); await p
  p = e.p.checkIssued(); e.requests[7].fail({ errMsg: 'offline' }); await p
  assert.equal(e.p.data.needsCheck, true); assert.equal(e.p.data.canConfirm, false)
})
test('409 clears old preview and cannot confirm without a new preview', async () => {
  const e = env(); await ready(e); await preview(e)
  const p = e.p.confirm(); respond(e, 6, null, 409); await p
  assert.equal(e.p.data.bill, null); assert.equal(e.p.token, ''); await e.p.confirm(); assert.equal(e.requests.length, 7)
})
test('readonly and blocked drafts never enable confirmation', async () => {
  const e = env(); await ready(e, 'view'); await preview(e); await e.p.confirm(); assert.equal(e.requests.length, 6)
  const f = env(); await ready(f); await preview(f, { ...view(), can_confirm: false, confirmation_token: '', block_reason: '请先抄表' }); await f.p.confirm(); assert.equal(f.requests.length, 6)
})
test('changing recipients and room invalidates preview token', async () => {
  const e = env(); await ready(e); await preview(e)
  e.p.selectTenants({ detail: { value: ['11'] } }); assert.equal(e.p.token, ''); assert.equal(e.p.data.bill, null)
  await preview(e); e.p.changeRoom(); assert.equal(e.p.data.room, null); assert.equal(e.p.token, '')
})
test('hidden confirmation response cannot restore private data', async () => {
  const e = env(); await ready(e); await preview(e)
  const p = e.p.confirm(); e.p.onHide(); respond(e, 6, { created: true, bill: { ...bill, id: 3 } }); await p
  assert.equal(e.p.data.bill, null); assert.equal(e.p.data.room, null); assert.equal(e.p.token, '')
})
test('new session rejects old401, while accepted current401 returns to login', async () => {
  const e = env(); await ready(e); await preview(e)
  let p = e.p.confirm(); e.session.start({ token: 'another-user', user: { id: 8, role: 'user' } }); e.p.clearPrivate(); respond(e, 6, null, 401); await p
  assert.equal(e.session.token(), 'another-user'); assert.equal(e.destinations.length, 0)
  const f = env(); await ready(f); await preview(f)
  f.session.subscribe(() => f.p.clearPrivate()); p = f.p.confirm(); respond(f, 6, null, 401); await p
  assert.equal(f.session.token(), ''); assert.ok(f.destinations.includes('/pages/login/index'))
})
test('revoked permission disables confirm without producing a successful bill', async () => {
  const e = env(); await ready(e); await preview(e)
  const p = e.p.confirm(); respond(e, 6, null, 403); await p
  assert.equal(e.p.data.canEdit, false); assert.equal(e.p.data.canConfirm, false); assert.equal(e.p.data.exists, false)
})
