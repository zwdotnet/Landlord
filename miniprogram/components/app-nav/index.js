const { logout } = require('../../lib/navigation')
const owner = [
  { key: 'home', title: '工作台', icon: 'view-module' },
  { key: 'rooms', title: '房源', icon: 'home' },
  { key: 'bills', title: '账单', icon: 'file' },
  { key: 'account', title: '我的', icon: 'user' },
]
const tenant = [
  { key: 'home', title: '租住', icon: 'home' },
  { key: 'bills', title: '账单', icon: 'file' },
  { key: 'contracts', title: '合同', icon: 'file-paste' },
  { key: 'profile', title: '我的', icon: 'user' },
]
Component({
  options: { styleIsolation: 'apply-shared' },
  properties: { role: { type: String, value: 'tenant' }, current: String, meters: Boolean, issue: Boolean, pending: Boolean },
  data: { items: tenant, more: false },
  observers: { role(role) { this.setData({ items: role === 'landlord' ? owner : tenant }) } },
  pageLifetimes: { hide() { this.setData({ more: false }) } },
  methods: {
    close() { this.setData({ more: false }) },
    visibleChange(e) { if (!e.detail.visible) this.close() },
    openMore() { this.setData({ more: true }) },
    select(e) {
      const key = e.currentTarget.dataset.key
      if (!this.data.items.some(row => row.key === key) || key === this.data.current) return
      this.close()
      if (this.data.role === 'landlord') wx.reLaunch({ url: '/pages/landlord/' + key + '/index' })
      else wx.switchTab({ url: '/pages/' + key + '/index' })
    },
    action(e) {
      const key = e.currentTarget.dataset.key
      this.close()
      if (key === 'logout') { logout(); return }
      if (key === 'resume') {
        const pages = getCurrentPages(), page = pages[pages.length - 1]
        if (page && page.resumePayment) page.resumePayment()
        return
      }
      const routes = this.data.role === 'landlord' ? { meters: '/pages/landlord/meters/index', issue: '/pages/landlord/issue/index', binding: '/pages/landlord/account/index' } : { payments: '/pages/payments/index', contact: '/pages/contact/index' }
      if (key === 'meters' && !this.data.meters || key === 'issue' && !this.data.issue) return
      if (routes[key]) {
        const pages = getCurrentPages(), page = pages[pages.length - 1]
        if (!page || '/' + page.route !== routes[key]) wx.navigateTo({ url: routes[key] })
      }
    },
  },
})
