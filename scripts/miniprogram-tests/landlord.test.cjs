const test = require('node:test')
const assert = require('node:assert/strict')
const path = require('node:path')
const root = path.resolve(__dirname, '../../miniprogram')
function env() {
  for (const k of Object.keys(require.cache)) if (k.startsWith(root + path.sep)) delete require.cache[k]
  const storage = new Map(), requests = [], destinations = []
  global.wx = { getStorageSync: k => storage.get(k), setStorageSync: (k, v) => storage.set(k, v), removeStorageSync: k => storage.delete(k), request: o => requests.push(o), reLaunch: o => destinations.push(o.url), switchTab: o => destinations.push(o.url), navigateTo: o => destinations.push(o.url) }
  global.getCurrentPages = () => []
  const session = require(root + '/lib/session'), landlord = require(root + '/lib/landlord')
  return { session, landlord, storage, requests, destinations }
}
const account = (role = 'user', id = 5, status = 1) => ({ token: 'synthetic-' + role + id, user: { id, username: role + id, role, status } })
const tick = () => new Promise(setImmediate)
function respond(e, index, data, status = 200) { e.requests[index].success({ statusCode: status, data: { code: status === 200 ? 0 : status, data, message: 'fixture denial' } }) }
function page(e, module) {
  let definition
  global.Page = o => { definition = o }
  require(root + module)
  const p = Object.assign({}, definition, { data: { ...definition.data }, setData(v) { Object.assign(this.data, v) } })
  if (p.onLoad) p.onLoad({})
  return p
}
test('landlord password login uses real auth endpoint, omits old bearer, clears password and dispatches server role', async () => {
  const e = env(), p = page(e, '/pages/login/index')
  p.switchMode({ currentTarget: { dataset: { mode: 'landlord' } } })
  p.inputUsername({ detail: { value: ' owner ' } }); p.inputPassword({ detail: { value: 'synthetic password' } })
  const pending = p.submitAccount(); await p.submitAccount()
  assert.equal(e.requests.length, 1); assert.equal(e.requests[0].header.Authorization, undefined)
  assert.equal(e.requests[0].data.username, 'owner'); assert.equal(e.requests[0].data.password, 'synthetic password')
  respond(e, 0, account()); await pending
  assert.equal(p.data.password, ''); assert.equal(p.data.busy, false)
  assert.equal(e.destinations.at(-1), '/pages/landlord/home/index')
  assert.ok(!JSON.stringify([...e.storage]).includes('synthetic password'))
})
test('hidden password response cannot establish a new session', async () => {
  const e = env(), p = page(e, '/pages/login/index')
  p.setData({ username: 'owner', password: 'synthetic' })
  const pending = p.submitAccount(); p.onHide(); respond(e, 0, account()); await pending
  assert.equal(e.session.token(), ''); assert.equal(p.data.password, ''); assert.equal(e.destinations.length, 0)
})
test('wrong password releases loading for retry without creating a session', async () => {
  const e = env(), p = page(e, '/pages/login/index')
  p.setData({ username: 'owner', password: 'incorrect' })
  const pending = p.submitAccount(); respond(e, 0, null, 401); await pending
  assert.equal(p.data.busy, false); assert.equal(e.session.token(), ''); assert.ok(p.data.error)
})
test('pending landlord is dispatched to approval screen and never creates a tenant account', async () => {
  const e = env(), p = page(e, '/pages/login/index')
  p.setData({ username: 'pending', password: 'synthetic' })
  const pending = p.submitAccount(); respond(e, 0, account('user', 7, 2)); await pending
  assert.equal(e.destinations.at(-1), '/pages/landlord/pending/index')
  assert.equal(e.requests.length, 1)
})
test('cold start uses authoritative role and approval status rather than cached label', async () => {
  const e = env(); e.session.start(account('tenant'))
  const p = page(e, '/pages/entry/index'), pending = p.refresh()
  respond(e, 0, { role: 'user', username: 'owner' }); await tick()
  assert.ok(e.requests[1].url.endsWith('/rental/users/me/approval'))
  respond(e, 1, { status: 2 }); await pending
  assert.equal(e.destinations.at(-1), '/pages/landlord/pending/index'); assert.ok(e.session.token())
})
test('hiding the entry page while approval loads prevents a late navigation', async () => {
  const e = env(); e.session.start(account())
  const p = page(e, '/pages/entry/index'), pending = p.refresh()
  respond(e, 0, { role: 'user' }); await tick(); p.onHide(); respond(e, 1, { status: 1 }); await pending
  assert.equal(e.destinations.length, 0); assert.ok(e.session.token())
})
test('entry network failure preserves the session and offers retry', async () => {
  const e = env(); e.session.start(account())
  const p = page(e, '/pages/entry/index'), pending = p.refresh()
  e.requests[0].fail({}); await pending
  assert.ok(e.session.token()); assert.equal(p.data.loading, false); assert.match(p.data.error, /连接失败/)
})
test('landlord entering tenant deep link is redirected without destroying the valid session', async () => {
  const e = env(); e.session.start(account())
  const pending = require(root + '/lib/page').guard()
  respond(e, 0, { role: 'admin' }); await tick(); respond(e, 1, { status: 1 })
  await assert.rejects(pending, { stale: true }); assert.ok(e.session.token())
  assert.equal(e.destinations.at(-1), '/pages/landlord/home/index')
})
test('tenant entering landlord deep link is routed to tenant home without a ledger request', async () => {
  const e = env(); e.session.start(account('tenant'))
  const pending = e.landlord.guard(false); respond(e, 0, { role: 'tenant' })
  await assert.rejects(pending, { stale: true }); assert.equal(e.requests.length, 1)
  assert.equal(e.destinations.at(-1), '/pages/home/index')
})
test('pending landlord guard cannot load ledgers or business data', async () => {
  const e = env(); e.session.start(account())
  const pending = e.landlord.guard(false); respond(e, 0, { role: 'user' }); await tick(); respond(e, 1, { status: 2 })
  await assert.rejects(pending, { stale: true }); assert.equal(e.requests.length, 2)
  assert.equal(e.destinations.at(-1), '/pages/landlord/pending/index')
})
test('scoped requests require selection, preserve ledger zero, and put scope in URL', async () => {
  const e = env(); e.session.start(account())
  await assert.rejects(e.landlord.request('/rental/rooms'), /选择台账/)
  e.landlord.select(0)
  const pending = e.landlord.request('/rental/rooms', { data: { page: 2 } })
  assert.ok(e.requests[0].url.endsWith('/rental/rooms?ledger_id=0'))
  assert.equal(e.requests[0].data.ledger_id, undefined); respond(e, 0, { items: [] }); await pending
  await assert.rejects(e.landlord.request('/rental/rooms?ledger_id=8'), /当前选择/)
})
test('late successful response or 401 after ledger switch cannot reveal data or clear the current session', async () => {
  for (const status of [200, 401]) {
    const e = env(); e.session.start(account()); e.landlord.select(1)
    const pending = e.landlord.request('/rental/bills'); e.landlord.select(2)
    respond(e, 0, { secret: 'old ledger' }, status)
    await assert.rejects(pending, { stale: true }); assert.ok(e.session.token())
  }
})
test('module denial keeps ledger picker available while preventing the business loader', async () => {
  const e = env(); e.session.start(account())
  const p = page(e, '/pages/landlord/bills/index'), pending = p.refresh()
  respond(e, 0, { role: 'user', username: 'owner' }); await tick(); respond(e, 1, { status: 1 }); await tick()
  respond(e, 2, [{ id: 9, name: 'only rooms', status: 'active', perms: { rooms: 'view' } }]); await pending
  assert.equal(e.requests.length, 3); assert.match(p.data.error, /没有此模块/)
  assert.equal(p.data.ledgerName, 'only rooms'); assert.equal(p.data.items.length, 0)
})
test('no active membership is an explicit empty workspace and invitations do not grant access', async () => {
  const e = env(); e.session.start(account())
  const p = page(e, '/pages/landlord/home/index'), pending = p.refresh()
  respond(e, 0, { role: 'user', username: 'owner' }); await tick(); respond(e, 1, { status: 1 }); await tick()
  respond(e, 2, [{ id: 9, name: 'pending invitation', status: 'pending', perms: { rooms: 'full' } }]); await pending
  assert.equal(p.data.hasLedger, false); assert.equal(p.data.canRooms, false); assert.equal(p.data.loading, false)
})
