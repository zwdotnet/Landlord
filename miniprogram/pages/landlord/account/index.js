const session = require('../../../lib/session')
const { request } = require('../../../lib/request')
const { enter, goLogin, logout } = require('../../../lib/navigation')
const { loginCode } = require('../../../lib/wechat')
const initial = () => ({ username: '', password: '', loading: false, busy: false, ready: false, bound: false, currentApp: false, available: false, status: 0, error: '', message: '' })
Page({
  data: initial(),
  onLoad() { this.sequence = 0 },
  onShow() { this.refresh() },
  onHide() { this.clearPrivate() },
  onUnload() { this.clearPrivate() },
  onPullDownRefresh() { if (!this.data.busy) this.refresh().finally(() => wx.stopPullDownRefresh()); else wx.stopPullDownRefresh() },
  clearPrivate() { this.sequence = (this.sequence || 0) + 1; this.setData(initial()) },
  inputPassword(e) { this.setData({ password: e.detail.value }) },
  async refresh() {
    if (this.data.busy) return
    const seq = ++this.sequence, state = session.snapshot(), current = () => seq === this.sequence && session.matches(state)
    this.setData(Object.assign(initial(), { loading: true }))
    try {
      const user = await request('/auth/me', { isCurrent: current })
      if (!current()) return
      if (!['user', 'admin'].includes(user.role)) { await enter(user, current); return }
      const binding = await request('/auth/wechat/bind', { isCurrent: current })
      if (current()) this.setData({ username: user.username, ready: true, bound: binding.bound, currentApp: binding.current_app, available: binding.available, status: binding.status })
    } catch (e) {
      if (!e.stale && seq === this.sequence) {
        if (e.status === 401 && !session.token()) goLogin()
        else this.setData({ error: e.message || '读取失败，请重试' })
      }
    } finally { if (seq === this.sequence) this.setData({ loading: false }) }
  },
  bind() { return this.mutate('bind') },
  unbind() { return this.mutate('unbind') },
  async mutate(mode) {
    if (this.data.busy || this.data.loading || !this.data.ready) return
    if (mode === 'bind' && (this.data.bound || !this.data.available || this.data.status !== 1)) return
    if (mode === 'unbind' && !this.data.bound) return
    const password = this.data.password
    if (!password) { this.setData({ error: '请输入当前账号密码' }); return }
    const seq = ++this.sequence, state = session.snapshot(), current = () => seq === this.sequence && session.matches(state)
    this.setData({ busy: true, password: '', error: '', message: '' })
    try {
      const data = { password }
      if (mode === 'bind') data.code = await loginCode()
      if (!current()) return
      const result = await request('/auth/wechat/bind', { method: mode === 'bind' ? 'POST' : 'DELETE', data, isCurrent: current })
      if (!current()) return
      if (result.requires_login) { session.clear(); goLogin(); return }
      this.setData({ bound: result.bound, currentApp: result.bound, message: result.bound ? '绑定成功，下次可使用微信快捷登录。' : '已解绑，可继续使用账号密码登录。' })
    } catch (e) {
      if (!e.stale && seq === this.sequence) {
        if (e.status === 401 && !session.token()) goLogin()
        else this.setData({ error: e.message || '操作未完成，请刷新状态后重试' })
      }
    } finally { if (seq === this.sequence) this.setData({ busy: false, password: '' }) }
  },
  logout,
})
