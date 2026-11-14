const session = require('./session')
const { request, error } = require('./request')
const { enter, goLogin, logout } = require('./navigation')
const intent = require('./payment-intent')
const files = require('./files')
let selected = null, epoch = 0, lastToken = session.token()
session.subscribe(() => { if (lastToken !== session.token()) { selected = null; epoch++; lastToken = session.token() } })
function select(id) {
  if (!Number.isInteger(Number(id)) || Number(id) < 0) throw error('无效的台账', 400)
  if (selected !== Number(id)) { selected = Number(id); epoch++ }
}
function snapshot() { return { epoch, id: selected } }
function matches(state) { return state.epoch === epoch && state.id === selected }
async function scopedRequest(path, options = {}) {
  if (selected == null) throw error('请先选择台账', 403)
  if (/[?&]ledger_id=/.test(path) || (options.data && Object.prototype.hasOwnProperty.call(options.data, 'ledger_id'))) throw error('台账参数由当前选择决定', 400)
  const state = snapshot()
  try {
    const data = await request(path + (path.includes('?') ? '&' : '?') + 'ledger_id=' + selected, Object.assign({}, options, { isCurrent: () => matches(state) && (!options.isCurrent || options.isCurrent()) }))
    if (!matches(state)) throw error('台账已切换，请重新加载', 0, true)
    return data
  } catch (e) {
    if (!matches(state) && !(e.status === 401 && !e.stale && !session.token())) throw error('台账已切换，请重新加载', 0, true)
    throw e
  }
}
async function guard(pending, isCurrent = () => true) {
  if (!session.token()) throw error('请先登录', 401)
  const user = await request('/auth/me', { isCurrent })
  if (!['user', 'admin'].includes(user.role)) { await enter(user, isCurrent); throw error('已切换到租客页面', 0, true) }
  const approval = await request('/rental/users/me/approval', { isCurrent })
  if (Number(approval.status) === 2 && !pending) { wx.reLaunch({ url: '/pages/landlord/pending/index' }); throw error('等待账号审核', 0, true) }
  if (Number(approval.status) === 1 && pending) { wx.reLaunch({ url: '/pages/landlord/home/index' }); throw error('账号已开通', 0, true) }
  if (![1, 2].includes(Number(approval.status))) { session.clear(); throw error('账号已不可用，请重新登录', 401) }
  if (pending) return { user, ledgers: [], ledger: null }
  const ledgers = (await request('/rental/ledgers/mine', { isCurrent })).filter(row => row.status === 'active')
  let ledger = ledgers.find(row => row.id === selected)
  if (!ledger) {
    ledger = ledgers[0] || null
    if (ledger) select(ledger.id)
    else if (selected !== null) { selected = null; epoch++ }
  }
  return { user, ledgers, ledger }
}
function canView(ledger, module) { return !!ledger && ['view', 'edit', 'full'].includes((ledger.perms || {})[module]) }
function canEdit(ledger, module) { return !!ledger && ['edit', 'full'].includes((ledger.perms || {})[module]) }
function landlordPage(options) {
  const initial = () => Object.assign({ loading: false, opening: false, error: '', username: '', ledgers: [], ledgerIndex: 0, ledgerName: '', hasLedger: false, canRooms: false, canBills: false, canMeters: false, canIssue: false, hasPendingPayment: false }, options.initial())
  Page(Object.assign({}, options.methods || {}, {
    data: initial(),
    onLoad(query) { this.query = query || {}; this.sequence = 0 },
    onShow() { this.refresh() },
    onHide() { this.clearPrivate() },
    onUnload() { this.clearPrivate() },
    onPullDownRefresh() { this.refresh().finally(() => wx.stopPullDownRefresh()) },
    clearPrivate() { this.sequence = (this.sequence || 0) + 1; this.targetPage = 1; this.keyword = ''; this.setData(initial()) },
    async refresh() {
      const seq = this.sequence = (this.sequence || 0) + 1, auth = session.snapshot()
      this.setData(Object.assign(initial(), { loading: true }))
      try {
        const context = await guard(!!options.pending, () => this.sequence === seq && session.matches(auth))
        if (this.sequence !== seq || !session.matches(auth)) return
        const scope = snapshot(), ledger = context.ledger
        const common = { username: context.user.username, ledgers: context.ledgers, ledgerIndex: Math.max(0, context.ledgers.findIndex(row => ledger && row.id === ledger.id)), ledgerName: ledger ? ledger.name : '', hasLedger: !!ledger, canRooms: canView(ledger, 'rooms'), canBills: canView(ledger, 'bills'), canMeters: canView(ledger, 'meters'), canIssue: canEdit(ledger, 'bills') }
        common.hasPendingPayment = !!intent.load()
        this.setData(common)
        if (options.module && !canView(ledger, options.module)) throw error(ledger ? '当前台账没有此模块的查看权限' : '暂无可用台账，请联系台账管理者', 403)
        const data = await options.load.call(this, context)
        if (this.sequence === seq && session.matches(auth) && matches(scope)) this.setData(Object.assign(common, data, { loading: false }))
      } catch (e) {
        if (e.status === 401 && !session.token()) goLogin()
        if (this.sequence === seq) this.setData({ loading: false, error: e.stale ? '' : e.message || '加载失败，请重试' })
      }
    },
    changeLedger(e) {
      const row = this.data.ledgers[Number(e.detail.value)]
      if (!row) return
      select(row.id); this.targetPage = 1; this.keyword = ''
      this.setData(initial())
      if (this.query.id) wx.reLaunch({ url: '/pages/landlord/bills/index' })
      else this.refresh()
    },
    goHome() { wx.reLaunch({ url: '/pages/landlord/home/index' }) },
    goAccount() { wx.navigateTo({ url: '/pages/landlord/account/index' }) },
    goRooms() { wx.navigateTo({ url: '/pages/landlord/rooms/index' }) },
    goMeters() { wx.navigateTo({ url: '/pages/landlord/meters/index' }) },
    goIssue() { wx.navigateTo({ url: '/pages/landlord/issue/index' }) },
    goBills() { wx.navigateTo({ url: '/pages/landlord/bills/index' }) },
    goBill(e) { const id = String(e.currentTarget.dataset.id); if (/^[1-9]\d*$/.test(id)) wx.navigateTo({ url: '/pages/landlord/bill/index?id=' + id }) },
    goPay() { if (this.data.canIssue && this.data.bill) wx.navigateTo({ url: '/pages/landlord/pay/index?id=' + this.data.bill.id }) },
    resumePayment() {
      const pending = intent.load()
      if (!pending) return this.refresh()
      if (!this.data.ledgers.some(row => row.id === pending.ledgerID && canView(row, 'bills'))) { wx.showToast({ title: '原台账无可用账单权限，请联系台账管理者后继续核对', icon: 'none' }); return }
      select(pending.ledgerID); wx.navigateTo({ url: '/pages/landlord/pay/index?id=' + pending.billID })
    },
    async openReceipt(e) {
      if (this.data.opening) return
      const id = Number(e.currentTarget.dataset.id)
      if (!(this.data.payments || []).some(p => p.receipt_id === id)) return
      const seq = this.sequence, auth = session.snapshot(), scope = snapshot(), current = () => seq === this.sequence && session.matches(auth) && matches(scope)
      this.setData({ opening: true })
      try { await files.openReceipt(id, scope.id, current) }
      catch (e) { if (e.status === 401 && !e.stale && !session.token()) goLogin(); else if (!e.stale && current()) wx.showToast({ title: e.message, icon: 'none' }) }
      finally { if (current()) this.setData({ opening: false }) }
    },
    logout,
  }))
}
module.exports = { select, snapshot, matches, request: scopedRequest, guard, landlordPage, canView, canEdit }
