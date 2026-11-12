// Read-only data suites authenticate pre-created synthetic accounts directly.
// This is fixture setup, not a test of the WeChat login page.
async function fixtureLogin(state, username, password = state.password) {
  const base = new URL(state.base)
  if (!['127.0.0.1', 'localhost'].includes(base.hostname)) throw Error('Fixture API must be loopback')
  const res = await fetch(base.href.replace(/\/$/, '') + '/auth/login', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ username, password }),
  })
  const body = await res.json()
  if (!res.ok || body.code !== 0) throw Error(body.message || 'Fixture login failed')
  return body.data
}
async function nativeFixtureLogin(mp, state, username, password) {
  const result = await fixtureLogin(state, username, password)
  await mp.evaluate(data => {
    const u = data.user
    wx.setStorageSync(['landlord','tenant','session','v1'].join('.'), { token: data.token, user: { id:u.id, username:u.username, role:u.role } })
    wx.reLaunch({ url:'/pages/home/index' })
    return true
  }, result)
}
module.exports = { fixtureLogin, nativeFixtureLogin }
