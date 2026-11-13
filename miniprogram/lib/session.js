const KEY = ['landlord', 'tenant', 'session', 'v1'].join('.')
let epoch = 0
const listeners = []
function current() { return wx.getStorageSync(KEY) || null }
function token() { const value = current(); return value && value.token || '' }
function snapshot() { return { epoch, token: token() } }
function matches(value) { return value.epoch === epoch && value.token === token() }
function notify() { epoch++; listeners.forEach(fn => fn()) }
function clear() { wx.removeStorageSync(KEY); notify() }
function start(result) {
  if (!result || !result.token || !result.user || !['tenant', 'user', 'admin'].includes(result.user.role)) {
    throw new Error('登录账号不可用，请重新登录')
  }
  clear()
  const u = result.user
  wx.setStorageSync(KEY, { token: result.token, user: { id: u.id, username: u.username, role: u.role } })
  notify()
}
function subscribe(fn) { listeners.push(fn); return () => { const i = listeners.indexOf(fn); if (i >= 0) listeners.splice(i, 1) } }
module.exports = { current, token, snapshot, matches, clear, start, subscribe }
