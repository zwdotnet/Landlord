const { request } = require('../../lib/request')
const session = require('../../lib/session')
const initial = () => ({ phone: '', phoneRequired: false, busy: false, error: '' })
Page({
  data: initial(),
  onLoad() { this.attempt = 0 },
  onHide() { this.clearPrivate() },
  onUnload() { this.clearPrivate() },
  onPullDownRefresh() { wx.stopPullDownRefresh() },
  clearPrivate() { this.attempt = (this.attempt || 0) + 1; this.setData(initial()) },
  input(e) { this.setData({ phone: e.detail.value }) },
  async submit() {
    if (this.data.busy) return
    const phone = this.data.phone.trim()
    if (this.data.phoneRequired && !/^1[3-9]\d{9}$/.test(phone)) {
      this.setData({ error: '请填写正确的入住手机号' }); return
    }
    const attempt = ++this.attempt, state = session.snapshot()
    this.setData({ busy: true, error: '' })
    try {
      const code = await new Promise((resolve, reject) => wx.login({
        timeout: 10000,
        success: result => result.code ? resolve(result.code) : reject(new Error('未取得微信登录凭证，请重试')),
        fail: () => reject(new Error('微信登录未完成，请重试')),
      }))
      if (attempt !== this.attempt || !session.matches(state)) return
      const data = await request('/auth/wechat/login', { public: true, method: 'POST', data: { code, phone } })
      if (attempt !== this.attempt || !session.matches(state)) return
      if (data.phone_required) {
        this.setData({ phoneRequired: true }); return
      }
      session.start(data)
      this.setData({ phone: '' })
      wx.switchTab({ url: '/pages/home/index' })
    } catch (e) {
      if (!e.stale && attempt === this.attempt) this.setData({ error: e.message || '登录失败，请重试' })
    } finally { if (attempt === this.attempt) this.setData({ busy: false }) }
  },
})
