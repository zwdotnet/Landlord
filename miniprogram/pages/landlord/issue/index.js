const landlord = require('../../../lib/landlord')
const session = require('../../../lib/session')
const { currentMonth, validPeriod } = require('../../../lib/meter-reading')
const fmt = require('../../../lib/format')
const { goLogin, logout } = require('../../../lib/navigation')
const emptyDraft = () => ({ bill: null, fees: [], exists: false, canConfirm: false, blockReason: '', needsCheck: false, message: '', formError: '' })
const emptySelection = () => Object.assign({ room: null, tenants: [], tenantIDs: [], contextLoading: false, selectionReady: false }, emptyDraft())
const initial = () => Object.assign({ loading: false, busy: false, error: '', username: '', ledgers: [], ledgerIndex: 0, ledgerName: '', hasLedger: false, canEdit: false, items: [], total: 0, page: 1, pages: 1, keyword: '', period: currentMonth() }, emptySelection())
Page({
  data: initial(),
  onLoad() { this.sequence = 0; this.operation = 0; this.targetPage = 1; this.keyword = '' },
  onShow() { this.refresh() },
  onHide() { this.clearPrivate() },
  onUnload() { this.clearPrivate() },
  onPullDownRefresh() { if (!this.data.busy) this.refresh().finally(() => wx.stopPullDownRefresh()); else wx.stopPullDownRefresh() },
  clearPrivate() { this.sequence = (this.sequence || 0) + 1; this.operation = (this.operation || 0) + 1; this.token = ''; this.targetPage = 1; this.keyword = ''; this.setData(initial()) },
  resetDraft() { this.operation++; this.token = ''; this.setData(emptyDraft()) },
  handleError(e, seq, field) {
    if (e.status === 401 && !e.stale && !session.token()) { goLogin(); return }
    if (e.stale || seq !== this.sequence) return
    if (e.status === 403) { this.token = ''; this.setData({ canEdit: false, canConfirm: false }) }
    this.setData({ [field]: e.message || '读取失败，请重试' })
  },
  async refresh() {
    if (this.data.busy) return
    const seq = ++this.sequence, auth = session.snapshot(), current = () => seq === this.sequence && session.matches(auth)
    const period = this.data.period, keyword = this.keyword || '', page = this.targetPage || 1
    this.resetDraft(); this.setData(Object.assign(initial(), { loading: true, period, keyword }))
    try {
      const { ledger, ledgers, user } = await landlord.guard(false, current)
      if (!current()) return
      this.setData({ username: user.username, ledgers, ledgerIndex: Math.max(0, ledgers.findIndex(row => ledger && row.id === ledger.id)), ledgerName: ledger ? ledger.name : '', hasLedger: !!ledger, canEdit: landlord.canEdit(ledger, 'bills') })
      if (!landlord.canView(ledger, 'bills')) throw new Error(ledger ? '当前台账没有账单查看权限' : '暂无可用台账，请联系台账管理者')
      const rooms = await landlord.request('/rental/bills/issue-rooms', { data: { keyword, page, pageSize: 10 }, isCurrent: current })
      if (current()) this.setData({ items: rooms.items, total: rooms.total, page, pages: Math.max(1, Math.ceil(rooms.total / 10)) })
    } catch (e) { this.handleError(e, seq, 'error') }
    finally { if (seq === this.sequence) this.setData({ loading: false }) }
  },
  changeLedger(e) {
    if (this.data.busy || this.data.contextLoading) return
    const row = this.data.ledgers[Number(e.detail.value)]
    if (row) { landlord.select(row.id); this.targetPage = 1; this.keyword = ''; return this.refresh() }
  },
  changePeriod(e) {
    if (this.data.busy || this.data.contextLoading || !validPeriod(e.detail.value)) return
    const room = this.data.room
    this.resetDraft(); this.setData(Object.assign(emptySelection(), { period: e.detail.value }))
    if (room) return this.loadTenants(room)
  },
  inputKeyword(e) { if (!this.data.busy) { this.keyword = e.detail.value; this.setData({ keyword: this.keyword }) } },
  search() { if (!this.data.busy) { this.targetPage = 1; return this.refresh() } },
  previousPage() { if (!this.data.busy) { this.targetPage = Math.max(1, this.data.page - 1); return this.refresh() } },
  nextPage() { if (!this.data.busy) { this.targetPage = Math.min(this.data.pages, this.data.page + 1); return this.refresh() } },
  chooseRoom(e) {
    if (this.data.busy || this.data.contextLoading) return
    const room = this.data.items.find(row => row.id === Number(e.currentTarget.dataset.id))
    if (room) return this.loadTenants(room)
  },
  changeRoom() { if (!this.data.busy && !this.data.contextLoading) { this.resetDraft(); this.setData(emptySelection()) } },
  async loadTenants(room) {
    this.resetDraft(); this.setData(Object.assign(emptySelection(), { room, contextLoading: true }))
    const op = this.operation, seq = this.sequence, auth = session.snapshot(), current = () => op === this.operation && seq === this.sequence && session.matches(auth)
    try {
      const data = await landlord.request('/rental/bills/issue-tenants', { data: { room_id: room.id, period: this.data.period }, isCurrent: current })
      if (current()) this.setData({ tenants: data.tenants.map(row => Object.assign({}, row, { selected: false })), selectionReady: true })
    } catch (e) { this.handleError(e, seq, 'formError') }
    finally { if (current()) this.setData({ contextLoading: false }) }
  },
  reloadTenants() { if (!this.data.busy && this.data.room) return this.loadTenants(this.data.room) },
  selectTenants(e) {
    if (this.data.busy || this.data.contextLoading) return
    const ids = e.detail.value.map(Number).filter(id => this.data.tenants.some(row => row.id === id))
    this.resetDraft(); this.setData({ tenantIDs: ids, tenants: this.data.tenants.map(row => Object.assign({}, row, { selected: ids.includes(row.id) })) })
  },
  selection() { return { room_id: this.data.room.id, period: this.data.period, tenant_ids: this.data.tenantIDs.join(',') } },
  applyPreview(view) {
    this.token = view.confirmation_token || ''
    this.setData({ bill: fmt.bill(view.bill), fees: view.items.map(row => Object.assign({}, row, { amountText: fmt.money(row.amount) })), exists: view.exists, canConfirm: !!view.can_confirm && !!this.token && this.data.canEdit, blockReason: view.block_reason || '', needsCheck: false, formError: '' })
  },
  async preview() {
    if (this.data.busy || this.data.contextLoading || this.data.needsCheck) return
    if (!this.data.room || !this.data.tenantIDs.length) { this.setData({ formError: '请选择本期承租关系' }); return }
    this.resetDraft()
    const seq = this.sequence, op = this.operation, auth = session.snapshot(), current = () => seq === this.sequence && op === this.operation && session.matches(auth)
    this.setData({ busy: true })
    try {
      const view = await landlord.request('/rental/bills/issue-preview', { data: this.selection(), isCurrent: current })
      if (current()) this.applyPreview(view)
    } catch (e) { this.handleError(e, seq, 'formError') }
    finally { if (current()) this.setData({ busy: false }) }
  },
  async confirm() {
    if (this.data.busy || !this.data.canEdit || !this.data.canConfirm || !this.token || this.data.needsCheck) return
    const token = this.token; this.token = ''
    const seq = this.sequence, op = ++this.operation, auth = session.snapshot(), current = () => seq === this.sequence && op === this.operation && session.matches(auth)
    this.setData({ busy: true, canConfirm: false, formError: '', message: '' })
    try {
      const result = await landlord.request('/rental/bills/confirm', { method: 'POST', data: { confirmation_token: token }, isCurrent: current })
      if (current()) this.setData({ bill: fmt.bill(result.bill), exists: true, fees: [], blockReason: result.message || '', message: result.created ? '已确认生成账单。' : '本期已有账单，已返回原单，请核对详情。' })
    } catch (e) {
      if (!e.stale && current() && (!e.status || e.status >= 500)) this.setData({ needsCheck: true, formError: '出账结果尚未确认，请先核对是否已出账。请勿重复提交。' })
      else if (!e.stale && current() && e.status === 409) this.setData(Object.assign(emptyDraft(), { formError: '预览已变化或过期，请重新预览后再确认。' }))
      else this.handleError(e, seq, 'formError')
    } finally { if (current()) this.setData({ busy: false }) }
  },
  async checkIssued() {
    if (this.data.busy || !this.data.needsCheck || !this.data.room) return
    const seq = this.sequence, op = ++this.operation, auth = session.snapshot(), current = () => seq === this.sequence && op === this.operation && session.matches(auth)
    this.setData({ busy: true, formError: '' })
    try {
      const view = await landlord.request('/rental/bills/issue-preview', { data: this.selection(), isCurrent: current })
      if (current()) { this.applyPreview(view); this.setData({ message: view.exists ? '已查到本期原单，请核对账单详情。' : '尚未查到本期账单，已重新读取预览；请核对后再明确确认。' }) }
    } catch (e) { this.handleError(e, seq, 'formError') }
    finally { if (current()) this.setData({ busy: false }) }
  },
  goBill() { if (!this.data.busy && this.data.exists && this.data.bill && this.data.bill.id) wx.navigateTo({ url: '/pages/landlord/bill/index?id=' + this.data.bill.id }) },
  goHome() { if (!this.data.busy) wx.reLaunch({ url: '/pages/landlord/home/index' }) },
  logout() { if (!this.data.busy) logout() },
})
