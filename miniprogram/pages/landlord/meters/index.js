const landlord = require('../../../lib/landlord')
const session = require('../../../lib/session')
const reading = require('../../../lib/meter-reading')
const { goLogin, logout } = require('../../../lib/navigation')
const emptyForm = () => ({ room: null, previous: null, currentRecord: null, contextLoading: false, formReady: false, water: '', elec: '', gas: '', note: '', busy: false, needsCheck: false, conflict: false, serverReading: '', formError: '', message: '', billExists: false, lowerReading: false })
const initial = () => Object.assign({ loading: false, error: '', username: '', ledgers: [], ledgerIndex: 0, ledgerName: '', hasLedger: false, canMeters: false, canEdit: false, items: [], total: 0, page: 1, pages: 1, keyword: '', period: reading.currentMonth() }, emptyForm())
Page({
  data: initial(),
  onLoad() { this.sequence = 0; this.operation = 0; this.targetPage = 1; this.keyword = '' },
  onShow() { this.refresh() },
  onHide() { this.clearPrivate() },
  onUnload() { this.clearPrivate() },
  onPullDownRefresh() { if (!this.data.busy) this.refresh().finally(() => wx.stopPullDownRefresh()); else wx.stopPullDownRefresh() },
  clearPrivate() { this.sequence = (this.sequence || 0) + 1; this.operation = (this.operation || 0) + 1; this.base = null; this.pending = null; this.targetPage = 1; this.keyword = ''; this.setData(initial()) },
  resetForm() { this.operation++; this.base = null; this.pending = null; this.setData(emptyForm()) },
  async refresh() {
    if (this.data.busy) return
    const seq = ++this.sequence, auth = session.snapshot(), current = () => seq === this.sequence && session.matches(auth)
    const period = this.data.period, keyword = this.keyword || '', page = this.targetPage || 1
    this.resetForm(); this.setData(Object.assign(initial(), { loading: true, period, keyword }))
    try {
      const context = await landlord.guard(false, current)
      if (!current()) return
      const { ledger, ledgers, user } = context
      this.setData({ username: user.username, ledgers, ledgerIndex: Math.max(0, ledgers.findIndex(row => ledger && row.id === ledger.id)), ledgerName: ledger ? ledger.name : '', hasLedger: !!ledger, canMeters: landlord.canView(ledger, 'meters'), canEdit: landlord.canEdit(ledger, 'meters') })
      if (!landlord.canView(ledger, 'meters')) throw new Error(ledger ? '当前台账没有抄表查看权限' : '暂无可用台账，请联系台账管理者')
      const rooms = await landlord.request('/rental/meter-records/rooms', { data: { keyword, page, pageSize: 10 }, isCurrent: current })
      if (current()) this.setData({ items: rooms.items, total: rooms.total, page, pages: Math.max(1, Math.ceil(rooms.total / 10)) })
    } catch (e) { this.handleError(e, seq, 'error') }
    finally { if (seq === this.sequence) this.setData({ loading: false }) }
  },
  handleError(e, seq, field) {
    if (e.status === 401 && !e.stale && !session.token()) { goLogin(); return }
    if (e.stale || seq !== this.sequence) return
    this.setData({ [field]: e.message || '读取失败，请重试' })
  },
  changeLedger(e) {
    if (this.data.busy || this.data.contextLoading) return
    const row = this.data.ledgers[Number(e.detail.value)]
    if (!row) return
    landlord.select(row.id); this.targetPage = 1; this.keyword = ''; this.refresh()
  },
  changePeriod(e) {
    if (this.data.busy || this.data.contextLoading || !reading.validPeriod(e.detail.value)) return
    const room = this.data.room
    this.resetForm(); this.setData({ period: e.detail.value })
    if (room) this.loadContext(room)
  },
  inputKeyword(e) { if (!this.data.busy) { this.keyword = e.detail.value; this.setData({ keyword: this.keyword }) } },
  search() { if (!this.data.busy) { this.targetPage = 1; return this.refresh() } },
  previousPage() { if (!this.data.busy) { this.targetPage = Math.max(1, this.data.page - 1); return this.refresh() } },
  nextPage() { if (!this.data.busy) { this.targetPage = Math.min(this.data.pages, this.data.page + 1); return this.refresh() } },
  chooseRoom(e) {
    if (this.data.busy || this.data.contextLoading) return
    const room = this.data.items.find(row => row.id === Number(e.currentTarget.dataset.id))
    if (room) return this.loadContext(room)
  },
  async loadContext(room) {
    if (this.data.busy) return
    this.resetForm(); this.setData({ room, contextLoading: true })
    const op = this.operation, seq = this.sequence, auth = session.snapshot(), current = () => op === this.operation && seq === this.sequence && session.matches(auth)
    try {
      const context = await landlord.request('/rental/meter-records/context', { data: { room_id: room.id, period: this.data.period }, isCurrent: current })
      if (current()) this.applyContext(context)
    } catch (e) { this.handleError(e, seq, 'formError') }
    finally { if (seq === this.sequence && op === this.operation) this.setData({ contextLoading: false }) }
  },
  applyContext(context) {
    const rec = context.current
    this.base = reading.expected(rec); this.pending = null
    this.setData({ room: context.room, previous: context.previous, currentRecord: rec, formReady: true, billExists: context.bill_exists, water: rec ? String(rec.water) : '', elec: rec ? String(rec.elec) : '', gas: rec ? String(rec.gas) : '', note: rec && rec.note !== '随账单生成' ? rec.note : '', needsCheck: false, conflict: false, serverReading: '', formError: '', message: '', lowerReading: false })
    this.updateLowerReading()
  },
  updateLowerReading() {
    const prev = this.data.previous
    this.setData({ lowerReading: !!prev && ['water', 'elec', 'gas'].some(key => this.data[key] !== '' && Number.isFinite(Number(this.data[key])) && Number(this.data[key]) < Number(prev[key])) })
  },
  inputReading(e) {
    if (this.data.busy || this.data.needsCheck || !this.data.canEdit) return
    const field = e.currentTarget.dataset.field
    if (!['water', 'elec', 'gas', 'note'].includes(field)) return
    this.setData({ [field]: e.detail.value, formError: '', message: '' }); this.updateLowerReading()
  },
  async save() {
    if (this.data.busy || this.data.contextLoading || !this.data.formReady || !this.data.canEdit || this.data.needsCheck) return
    let payload
    try { payload = reading.payload(this.data, this.base) } catch (e) { this.setData({ formError: e.message }); return }
    const seq = this.sequence, op = ++this.operation, auth = session.snapshot(), current = () => seq === this.sequence && op === this.operation && session.matches(auth)
    this.pending = { payload, base: this.base }
    this.setData({ busy: true, formError: '', message: '', serverReading: '' })
    try {
      await landlord.request('/rental/meter-records', { method: 'POST', data: payload, isCurrent: current })
      if (current()) await this.reconcile(current)
    } catch (e) {
      if (!e.stale && current()) {
        if (!e.status || e.status >= 500 || e.status === 409) this.setData({ needsCheck: true, formError: e.status === 409 ? e.message : '保存结果尚未确认，请先核对服务器记录。请勿重复提交。' })
        else { if (e.status === 403) this.setData({ canEdit: false }); this.handleError(e, seq, 'formError') }
      } else if (e.status === 401 && !e.stale && !session.token()) goLogin()
    } finally { if (seq === this.sequence && op === this.operation) this.setData({ busy: false }) }
  },
  async reconcile(current) {
    const pending = this.pending
    if (!pending) return
    try {
      const context = await landlord.request('/rental/meter-records/context', { data: { room_id: pending.payload.room_id, period: pending.payload.period }, isCurrent: current })
      if (!current()) return
      if (reading.sameReading(context.current, pending.payload)) {
        this.applyContext(context); this.setData({ message: '已回查服务器，读数与本次输入一致。' })
      } else if (reading.sameVersion(context.current, pending.base)) {
        this.base = reading.expected(context.current); this.pending = null
        this.setData({ previous: context.previous, billExists: context.bill_exists, needsCheck: false, conflict: false, formError: '', message: '服务器记录尚未改变，输入已保留，可以重试保存。' })
      } else {
        const rec = context.current
        this.setData({ needsCheck: true, conflict: true, formError: '服务器记录已有变化，请核对后载入，再重新修改。', serverReading: rec ? `水 ${rec.water} · 电 ${rec.elec} · 燃气 ${rec.gas}` : '服务器上暂无本期记录' })
      }
    } catch (e) {
      if (!e.stale && current()) this.setData({ needsCheck: true, formError: '暂时无法核对服务器记录，网络恢复后请再次核对。' })
      throw e
    }
  },
  async checkSaved() {
    if (this.data.busy || !this.pending) return
    const seq = this.sequence, op = ++this.operation, auth = session.snapshot(), current = () => seq === this.sequence && op === this.operation && session.matches(auth)
    this.setData({ busy: true, formError: '' })
    try { await this.reconcile(current) } catch (e) { if (e.status === 401 && !e.stale && !session.token()) goLogin() }
    finally { if (seq === this.sequence && op === this.operation) this.setData({ busy: false }) }
  },
  reloadContext() { if (!this.data.busy && this.data.room) return this.loadContext(this.data.room) },
  goHome() { if (!this.data.busy) wx.reLaunch({ url: '/pages/landlord/home/index' }) },
  logout,
})
