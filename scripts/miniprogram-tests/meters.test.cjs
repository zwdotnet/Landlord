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
  session.start({ token: 'synthetic-owner-token', user: { id: 5, role: 'user', username: 'owner' } })
  let definition; global.Page = o => { definition = o }; require(root + '/pages/landlord/meters/index')
  const p = Object.assign({}, definition, { data: { ...definition.data }, setData(v) { Object.assign(this.data, v) } }); p.onLoad({})
  return { p, requests, destinations, session, landlord, storage }
}
function respond(e, index, data, status = 200) { e.requests[index].success({ statusCode: status, data: { code: status === 200 ? 0 : status, message: 'fixture denial', data } }) }
const record = (water = 20, updated = '2030-01-01T00:00:00Z') => ({ id: 11, room_id: 1, period: '2030-01', water, elec: 200, gas: 0, note: '', updated_at: updated })
const context = current => ({ room: { id: 1, room_no: 'M-01' }, previous: { period: '2029-11', is_initial: false, water: 10, elec: 100, gas: 0 }, current, bill_exists: false })
async function ready(e, tier = 'edit') {
  const pending = e.p.refresh()
  respond(e, 0, { role: 'user', username: 'owner' }); await tick()
  respond(e, 1, { status: 1 }); await tick()
  respond(e, 2, [{ id: 9, status: 'active', name: 'meter only', perms: { meters: tier } }]); await tick()
  respond(e, 3, { items: [{ id: 1, room_no: 'M-01' }], total: 1 }); await pending
  e.p.setData({ period: '2030-01' })
}
async function choose(e, current = null) {
  const pending = e.p.chooseRoom({ currentTarget: { dataset: { id: 1 } } })
  respond(e, 4, context(current)); await pending
}
function fill(e, water = '20') { for (const [field, value] of Object.entries({ water, elec: '200', gas: '0' })) e.p.inputReading({ currentTarget: { dataset: { field } }, detail: { value } }) }
test('meter-only member selects minimal room endpoint and reads server previous context', async () => {
  const e = env(); await ready(e); await choose(e)
  assert.equal(e.p.data.canEdit, true); assert.ok(e.requests[3].url.includes('/meter-records/rooms?ledger_id=9'))
  assert.equal(e.p.data.previous.period, '2029-11'); assert.equal(e.p.data.previous.water, 10)
  assert.equal(e.p.data.water, ''); assert.equal(e.p.data.formReady, true)
  assert.ok(!e.requests.some(r => /\/rooms\?/.test(r.url) && !r.url.includes('/meter-records/rooms')))
})
test('empty, negative or non-finite fields cannot silently become zero meter readings', async () => {
  const e = env(); await ready(e); await choose(e)
  for (const water of ['', '-1', 'Infinity', 'NaN', '0x10']) {
    fill(e, water); await e.p.save(); assert.ok(e.p.data.formError); assert.equal(e.requests.length, 5)
  }
  fill(e); e.p.setData({ gas: '' }); await e.p.save(); assert.equal(e.requests.length, 5)
})
test('save carries version and ledger in URL, blocks duplicate taps and switching until readback', async () => {
  const e = env(); await ready(e); await choose(e); fill(e)
  const pending = e.p.save(); await e.p.save(); e.p.goHome(); e.p.changeLedger({ detail: { value: 0 } })
  assert.equal(e.requests.length, 6); assert.equal(e.destinations.length, 0)
  assert.equal(e.requests[5].method, 'POST'); assert.ok(e.requests[5].url.endsWith('?ledger_id=9'))
  assert.deepEqual(e.requests[5].data.expected, { id: 0 }); assert.equal(e.requests[5].data.gas, 0)
  respond(e, 5, record()); await tick(); assert.equal(e.requests[6].method, 'GET'); assert.equal(e.p.data.busy, true)
  respond(e, 6, context(record())); await pending
  assert.equal(e.p.data.busy, false); assert.equal(e.p.data.currentRecord.id, 11); assert.ok(e.p.data.message)
  assert.ok(!JSON.stringify([...e.storage]).includes('M-01'))
})
test('new save of existing reading includes its exact server version', async () => {
  const e = env(); await ready(e); await choose(e, record()); fill(e, '21')
  const pending = e.p.save()
  assert.deepEqual(e.requests[5].data.expected, { id: 11, updated_at: record().updated_at })
  respond(e, 5, record(21)); await tick(); respond(e, 6, context(record(21))); await pending
})
test('response lost after save is reconciled without issuing a second POST', async () => {
  const e = env(); await ready(e); await choose(e); fill(e)
  const pending = e.p.save(); e.requests[5].fail({}); await pending
  assert.equal(e.p.data.needsCheck, true); await e.p.save(); assert.equal(e.requests.length, 6)
  const checking = e.p.checkSaved(); respond(e, 6, context(record())); await checking
  assert.equal(e.p.data.needsCheck, false); assert.ok(e.p.data.message)
  assert.equal(e.requests.filter(r => r.method === 'POST').length, 1)
})
test('request never saved can retry only after readback confirms original version', async () => {
  const e = env(); await ready(e); await choose(e, record()); fill(e, '21')
  const pending = e.p.save(); e.requests[5].fail({}); await pending
  const checking = e.p.checkSaved(); respond(e, 6, context(record())); await checking
  assert.equal(e.p.data.needsCheck, false); assert.equal(e.p.data.water, '21'); assert.match(e.p.data.message, /重试/)
  const retry = e.p.save(); assert.equal(e.requests[7].data.water, 21)
  respond(e, 7, record(21)); await tick(); respond(e, 8, context(record(21))); await retry
})
test('concurrent edit requires explicit reloading and preserves the unsaved input for comparison', async () => {
  const e = env(); await ready(e); await choose(e, record()); fill(e, '21')
  const pending = e.p.save(); respond(e, 5, null, 409); await pending
  const checking = e.p.checkSaved(); respond(e, 6, context(record(22, '2030-01-02T00:00:00Z'))); await checking
  assert.equal(e.p.data.conflict, true); assert.equal(e.p.data.water, '21'); assert.match(e.p.data.serverReading, /22/)
  await e.p.save(); assert.equal(e.requests.length, 7)
  const reload = e.p.reloadContext(); respond(e, 7, context(record(22, '2030-01-02T00:00:00Z'))); await reload
  assert.equal(e.p.data.water, '22'); assert.equal(e.p.data.conflict, false)
})
test('successful POST followed by failed GET remains uncertain until an explicit check', async () => {
  const e = env(); await ready(e); await choose(e); fill(e)
  const pending = e.p.save(); respond(e, 5, record()); await tick(); e.requests[6].fail({}); await pending
  assert.equal(e.p.data.needsCheck, true); assert.equal(e.p.data.busy, false); assert.equal(e.p.data.water, '20')
})
test('readonly meter member can inspect readings but cannot edit or call save', async () => {
  const e = env(); await ready(e, 'view'); await choose(e, record())
  fill(e, '30'); await e.p.save(); assert.equal(e.p.data.water, '20'); assert.equal(e.requests.length, 5)
})
test('revoked save permission releases loading and disables further writes', async () => {
  const e = env(); await ready(e); await choose(e); fill(e)
  const pending = e.p.save(); respond(e, 5, null, 403); await pending
  assert.equal(e.p.data.canEdit, false); assert.equal(e.p.data.busy, false); await e.p.save(); assert.equal(e.requests.length, 6)
})
test('hiding during a scoped context request rejects its late 401 without clearing the session', async () => {
  const e = env(); await ready(e)
  const pending = e.p.chooseRoom({ currentTarget: { dataset: { id: 1 } } }); e.p.onHide(); respond(e, 4, null, 401); await pending
  assert.ok(e.session.token()); assert.equal(e.p.data.room, null); assert.equal(e.p.data.previous, null)
})
test('hidden save response cannot expose old data or clear a replacement account', async () => {
  for (const status of [200, 401]) {
    const e = env(); await ready(e); await choose(e); fill(e)
    const pending = e.p.save(); e.p.onHide(); e.session.start({ token: 'new-owner-token', user: { id: 20, role: 'user', username: 'new' } })
    respond(e, 5, record(), status); await pending
    assert.equal(e.session.current().user.id, 20); assert.equal(e.p.data.room, null); assert.equal(e.p.data.water, '')
  }
})
test('hidden pending approval request cannot navigate back to a stale page', async () => {
  const e = env(), pending = e.p.refresh(); respond(e, 0, { role: 'user' }); await tick(); e.p.onHide(); respond(e, 1, { status: 2 }); await pending
  assert.equal(e.destinations.length, 0)
})
test('expired active save returns to login even when App session cleanup invalidates the page sequence', async () => {
  const e = env(); await ready(e); await choose(e); fill(e); e.session.subscribe(() => e.p.clearPrivate())
  const pending = e.p.save(); respond(e, 5, null, 401); await pending
  assert.equal(e.session.token(), ''); assert.equal(e.destinations.at(-1), '/pages/login/index')
})
test('month switch clears previous draft, and low reading is visibly marked for confirmation', async () => {
  const e = env(); await ready(e); await choose(e); fill(e, '9'); assert.equal(e.p.data.lowerReading, true)
  const pending = e.p.changePeriod({ detail: { value: '2030-02' } })
  assert.equal(e.p.data.water, ''); assert.equal(e.requests[5].data.period, '2030-02')
  respond(e, 5, context(null)); await tick(); assert.equal(e.p.data.formReady, true); await pending
})
