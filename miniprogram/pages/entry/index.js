const session = require('../../lib/session')
const { request } = require('../../lib/request')
const { enter, goLogin } = require('../../lib/navigation')
Page({
  data: { loading: true, error: '' },
  onShow() { this.refresh() },
  onHide() { this.sequence = (this.sequence || 0) + 1 },
  async refresh() {
    const seq = this.sequence = (this.sequence || 0) + 1
    if (!session.token()) { goLogin(); return }
    this.setData({ loading: true, error: '' })
    try {
      const user = await request('/auth/me')
      if (seq === this.sequence) await enter(user, () => seq === this.sequence)
    } catch (e) {
      if (e.status === 401 && !session.token()) goLogin()
      if (!e.stale && seq === this.sequence) this.setData({ loading: false, error: e.message || '连接失败，请重试' })
    }
  },
})
