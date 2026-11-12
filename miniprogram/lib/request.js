const config = require('../config')
const session = require('./session')
function error(message, status, stale) { return Object.assign(new Error(message), { status, stale: !!stale }) }
function url(path) {
  if (!/^\/(auth|rental)\//.test(path) || path.includes('..') || path.includes('://')) throw error('无效的请求路径')
  return config.apiBase.replace(/\/$/, '') + path
}
function request(path, options = {}) {
  const state = session.snapshot()
  const auth = !options.public
  if (auth && !state.token) return Promise.reject(error('请先登录', 401))
  return new Promise((resolve, reject) => {
    wx.request({
      url: url(path), method: options.method || 'GET', data: options.data,
      timeout: 25000,
      header: Object.assign({ 'Content-Type': 'application/json' }, auth ? { Authorization: 'Bearer ' + state.token } : {}),
      success(res) {
        if (!session.matches(state)) { reject(error('账号已切换，请重新加载', 0, true)); return }
        const body = res.data
        if (res.statusCode === 401 && auth) session.clear()
        if (res.statusCode < 200 || res.statusCode >= 300 || !body || body.code !== 0) {
          reject(error(body && typeof body.message === 'string' ? body.message : '服务暂不可用，请重试', res.statusCode))
          return
        }
        resolve(body.data)
      },
      fail() { reject(error(session.matches(state) ? '连接失败，请检查网络后重试' : '账号已切换', 0, !session.matches(state))) },
    })
  })
}
module.exports = { request, url, error }
