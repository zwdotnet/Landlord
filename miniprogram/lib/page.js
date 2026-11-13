const session = require('./session')
const { request, error } = require('./request')
const files = require('./files')
const { goLogin, enter, logout } = require('./navigation')
async function guard(pending = false) {
  if (!session.token()) throw error('请先登录', 401)
  const user = await request('/auth/me')
  if (user.role !== 'tenant') { await enter(user); throw error('已切换到账号对应的工作台', 0, true) }
  const status = await request('/rental/portal/status')
  if (!status.bound && !pending) {
    wx.reLaunch({ url: '/pages/pending/index' })
    throw error('等待房东关联', 403, true)
  }
  if (status.bound && pending) {
    wx.switchTab({ url: '/pages/home/index' })
    throw error('已关联', 0, true)
  }
  return status
}
function tenantPage(options) {
  const initial = () => Object.assign({ loading: false, error: '', opening: false }, options.initial())
  Page(Object.assign({}, options.methods || {}, {
    data: initial(),
    onLoad(query) { this.query = query || {}; this.sequence = 0 },
    onShow() { this.refresh() },
    onHide() { this.clearPrivate() },
    onUnload() { this.clearPrivate() },
    onPullDownRefresh() { this.refresh().finally(() => wx.stopPullDownRefresh()) },
    clearPrivate() { this.sequence = (this.sequence || 0) + 1; this.targetPage = 1; this.setData(initial()) },
    async refresh() {
      const seq = (this.sequence || 0) + 1
      this.sequence = seq
      const state = session.snapshot()
      this.setData(Object.assign(initial(), { loading: true }))
      try {
        const status = await guard(!!options.pending)
        const data = await options.load.call(this, status)
        if (session.matches(state) && this.sequence === seq) this.setData(Object.assign({}, data, { loading: false }))
      } catch (e) {
        if (e.status === 401 && !session.token()) goLogin()
        if (!e.stale && this.sequence === seq) this.setData({ error: e.message || '加载失败，请重试', loading: false })
      }
    },
    async openFile(e) {
      if (this.data.opening) return
      const seq = this.sequence
      this.setData({ opening: true })
      try {
        const d = e.currentTarget.dataset
        await guard()
        if (this.sequence !== seq) return
        await files.open(d.path, d.kind || 'pdf')
      } catch (err) {
        if (err.status === 401) goLogin()
        else if (!err.stale) wx.showToast({ title: err.message, icon: 'none' })
      } finally { if (this.sequence === seq) this.setData({ opening: false }) }
    },
    logout,
    goPayments() { wx.navigateTo({ url: '/pages/payments/index' }) },
    goBill(e) { const id = e.currentTarget.dataset.id; if (/^[1-9]\d*$/.test(String(id))) wx.navigateTo({ url: '/pages/bill/index?id=' + id }) },
    goBills() { wx.switchTab({ url: '/pages/bills/index' }) },
    goContracts() { wx.switchTab({ url: '/pages/contracts/index' }) },
  }))
}
module.exports = { tenantPage, guard, goLogin }
