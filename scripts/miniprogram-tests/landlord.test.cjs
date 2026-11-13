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

function mockCodes() {
  let calls = 0
  wx.login = o => { calls++; o.success({ code: 'fresh-code-' + calls }) }
  return () => calls
}
async function bindingPage(e, binding = { bound: false, current_app: false, available: true, status: 1 }) {
  const p = page(e, '/pages/landlord/account/index'), pending = p.refresh()
  respond(e, 0, { role: 'user', username: 'owner' }); await tick()
  respond(e, 1, binding); await pending
  return p
}
test('landlord WeChat quick login uses a fresh code, omits bearer and preserves pending role', async () => {
  const e = env(), codes = mockCodes(), p = page(e, '/pages/login/index')
  const pending = p.submitLandlordWeChat(); await p.submitLandlordWeChat(); await tick()
  assert.equal(codes(), 1); assert.equal(e.requests.length, 1)
  assert.ok(e.requests[0].url.endsWith('/auth/wechat/landlord-login')); assert.equal(e.requests[0].header.Authorization, undefined)
  assert.deepEqual(e.requests[0].data, { code: 'fresh-code-1' })
  respond(e, 0, account('user', 5, 2)); await pending
  assert.equal(e.destinations.at(-1), '/pages/landlord/pending/index')
})
test('hidden quick login or failure obtaining code cannot create a session', async () => {
  for (const fail of [false, true]) {
    const e = env(), p = page(e, '/pages/login/index')
    if (fail) wx.login = o => o.fail({})
    else mockCodes()
    const pending = p.submitLandlordWeChat(); await tick()
    if (!fail) { p.onHide(); respond(e, 0, account()) }
    await pending
    assert.equal(e.session.token(), ''); assert.equal(p.data.busy, false)
    if (fail) assert.ok(p.data.error)
  }
})
test('quick login still navigates when the app session subscriber clears the login form', async () => {
  const e = env(), p = page(e, '/pages/login/index'); mockCodes()
  e.session.subscribe(() => p.clearPrivate())
  const pending = p.submitLandlordWeChat(); await tick(); respond(e, 0, account()); await pending
  assert.ok(e.session.token()); assert.equal(p.data.password, '')
  assert.equal(e.destinations.at(-1), '/pages/landlord/home/index')
})
test('quick login conflict keeps password fallback available and retries with a new code', async () => {
  const e = env(), codes = mockCodes(), p = page(e, '/pages/login/index')
  for (let i = 0; i < 2; i++) {
    const pending = p.submitLandlordWeChat(); await tick(); respond(e, i, null, 403); await pending
    assert.equal(p.data.busy, false); assert.ok(p.data.error)
  }
  assert.equal(codes(), 2); assert.notEqual(e.requests[0].data.code, e.requests[1].data.code)
  p.setData({ username: 'owner', password: 'password' }); const pending = p.submitAccount()
  respond(e, 2, account()); await pending; assert.ok(e.session.token())
})
test('binding does not require ledger membership, requires password and never persists password or code', async () => {
  const e = env(); e.session.start(account()); const codes = mockCodes(), p = await bindingPage(e)
  await p.bind(); assert.equal(codes(), 0); assert.equal(e.requests.length, 2)
  p.setData({ password: 'confirmation password' }); const pending = p.bind(); await p.bind(); await tick()
  assert.equal(p.data.password, ''); assert.equal(e.requests.length, 3); assert.equal(codes(), 1)
  assert.equal(e.requests[2].method, 'POST'); assert.equal(e.requests[2].data.password, 'confirmation password')
  respond(e, 2, { bound: true, requires_login: false }); await pending
  assert.equal(p.data.bound, true); assert.ok(p.data.message); assert.equal(p.data.busy, false)
  assert.ok(!JSON.stringify([...e.storage]).includes('confirmation password')); assert.ok(!JSON.stringify([...e.storage]).includes('fresh-code'))
})
test('binding conflict can retry with a fresh code and clears loading and confirmation password', async () => {
  const e = env(); e.session.start(account()); const codes = mockCodes(), p = await bindingPage(e)
  for (let i = 2; i < 4; i++) {
    p.setData({ password: 'password' }); const pending = p.bind(); await tick(); respond(e, i, null, 409); await pending
    assert.equal(p.data.password, ''); assert.equal(p.data.bound, false); assert.equal(p.data.busy, false); assert.ok(p.data.error)
  }
  assert.equal(codes(), 2); assert.notEqual(e.requests[2].data.code, e.requests[3].data.code)
})
test('hiding binding page before wx.login returns prevents mutation and clears private fields', async () => {
  const e = env(); e.session.start(account()); const p = await bindingPage(e); let callback
  wx.login = o => { callback = o }; p.setData({ password: 'password' })
  const pending = p.bind(); p.onHide(); callback.success({ code: 'late-code' }); await pending
  assert.equal(e.requests.length, 2); assert.equal(p.data.password, ''); assert.equal(p.data.ready, false)
})
test('late binding status or mutation 401 cannot clear the replacement account session', async () => {
  for (const mutation of [false, true]) {
    const e = env(); e.session.start(account()); mockCodes()
    let p, pending, index
    if (mutation) { p = await bindingPage(e); p.setData({ password: 'password' }); pending = p.bind(); await tick(); index = 2 }
    else { p = page(e, '/pages/landlord/account/index'); pending = p.refresh(); respond(e, 0, { role: 'user' }); await tick(); index = 1 }
    e.session.start(account('user', 20)); p.onHide(); respond(e, index, null, 401); await pending
    assert.equal(e.session.current().user.id, 20); assert.equal(p.data.bound, false); assert.equal(p.data.password, '')
  }
})
test('unbind sends password without wx.login, clears local session and routes to password fallback', async () => {
  const e = env(); e.session.start(account()); const codes = mockCodes()
  const p = await bindingPage(e, { bound: true, current_app: true, available: true, status: 1 })
  p.setData({ password: 'password' }); const pending = p.unbind()
  assert.equal(e.requests[2].method, 'DELETE'); assert.equal(e.requests[2].data.code, undefined)
  respond(e, 2, { bound: false, requires_login: true }); await pending
  assert.equal(codes(), 0); assert.equal(e.session.token(), ''); assert.equal(p.data.password, '')
  assert.equal(e.destinations.at(-1), '/pages/login/index')
})
test('pending or unconfigured account cannot bind and tenant deep links return to original portal', async () => {
  for (const binding of [{ bound: false, available: true, status: 2 }, { bound: false, available: false, status: 1 }]) {
    const e = env(); e.session.start(account()); const codes = mockCodes(), p = await bindingPage(e, binding)
    p.setData({ password: 'password' }); await p.bind(); assert.equal(codes(), 0); assert.equal(e.requests.length, 2)
  }
  const e = env(); e.session.start(account('tenant')); const p = page(e, '/pages/landlord/account/index'), pending = p.refresh()
  respond(e, 0, { role: 'tenant' }); await pending
  assert.equal(e.requests.length, 1); assert.equal(e.destinations.at(-1), '/pages/home/index'); assert.ok(e.session.token())
})
