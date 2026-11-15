const session = require('../../lib/session')
const { request } = require('../../lib/request')
const { enter, goLogin } = require('../../lib/navigation')
const initial = () => ({ loading: false, busy: false, ready: false, phone: '', original: '', message: '', error: '', needsCheck: false })
Page({
  data: initial(),
  onLoad() { this.sequence = 0 },
  onShow() { this.refresh() },
  onHide() { this.clearPrivate() },
  onUnload() { this.clearPrivate() },
  onPullDownRefresh() { if (!this.data.busy) this.refresh().finally(() => wx.stopPullDownRefresh()); else wx.stopPullDownRefresh() },
  clearPrivate() { this.sequence = (this.sequence || 0) + 1; this.setData(initial()) },
  input(e) { if (!this.data.busy && !this.data.needsCheck) this.setData({ phone: e.detail.value, error: '', message: '' }) },
  goBack() { wx.navigateBack({ fail: () => wx.reLaunch({ url: '/pages/entry/index' }) }) },
  async refresh() {
    if (this.data.busy) return
    const seq = ++this.sequence, state = session.snapshot(), current = () => this.sequence === seq && session.matches(state)
    this.setData(Object.assign(initial(), { loading: true }))
    try {
      const user = await request('/auth/me', { isCurrent: current })
      if (!current()) return
      if (user.role !== 'tenant') { await enter(user, current); return }
      const data = await request('/auth/contact', { isCurrent: current })
      if (current()) this.setData({ ready: true, phone: data.phone, original: data.phone })
    } catch (e) {
      if (!e.stale && current()) this.setData({ error: e.message || '读取失败，请重试' })
      if (e.status === 401 && !session.token()) goLogin()
    } finally { if (current()) this.setData({ loading: false }) }
  },
  async save() {
    if (this.data.busy || !this.data.ready || this.data.loading || this.data.needsCheck) return
    const phone = this.data.phone.trim(), original = this.data.original
    if (!/^1[3-9]\d{9}$/.test(phone)) { this.setData({ error: '请填写正确的11位手机号' }); return }
    const seq = ++this.sequence, state = session.snapshot(), current = () => this.sequence === seq && session.matches(state)
    this.setData({ busy: true, error: '', message: '' })
    try {
      const data = await request('/auth/contact', { method: 'PUT', data: { phone, expected_phone: original }, isCurrent: current })
      if (current()) this.setData({ phone: data.phone, original: data.phone, message: '手机号已保存，返回后会显示新号码。' })
    } catch (e) {
      if (!e.stale && current()) this.setData({ error: e.message || '保存结果待核对', needsCheck: e.status === 0 || e.status >= 500 || e.status === 409 })
      if (e.status === 401 && !session.token()) goLogin()
    } finally { if (current()) this.setData({ busy: false }) }
  },
})
