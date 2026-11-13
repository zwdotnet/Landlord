const session = require('./session')
const { request, error } = require('./request')
function goLogin() {
  const pages = getCurrentPages()
  if (!pages.length || pages[pages.length - 1].route !== 'pages/login/index') wx.reLaunch({ url: '/pages/login/index' })
}
async function enter(user, isCurrent = () => true) {
  const state = session.snapshot()
  const check = () => { if (!session.matches(state) || !isCurrent()) throw error('页面或账号已切换', 0, true) }
  check()
  if (!user || !['tenant', 'user', 'admin'].includes(user.role)) {
    session.clear(); goLogin(); throw error('账号不可用，请重新登录', 401)
  }
  if (user.role === 'tenant') { wx.switchTab({ url: '/pages/home/index' }); return }
  const status = user.status == null ? (await request('/rental/users/me/approval')).status : user.status
  check()
  if (![1, 2].includes(Number(status))) { session.clear(); goLogin(); throw error('账号不可用，请重新登录', 401) }
  wx.reLaunch({ url: Number(status) === 2 ? '/pages/landlord/pending/index' : '/pages/landlord/home/index' })
}
function logout() {
  if (session.token()) request('/auth/logout', { method: 'POST' }).catch(() => {})
  session.clear(); goLogin()
}
module.exports = { goLogin, enter, logout }
