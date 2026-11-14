const landlord = require('../../../lib/landlord')
const session = require('../../../lib/session')
const intent = require('../../../lib/payment-intent')
const files = require('../../../lib/files')
const fmt = require('../../../lib/format')
const { goLogin, logout } = require('../../../lib/navigation')
const initial = () => ({ loading: false, busy: false, opening: false, error: '', formError: '', message: '', username: '', ledgers: [], ledgerIndex: 0, ledgerName: '', canEdit: false, bill: null, payments: [], amount: '', note: '', reviewed: false, reviewedAmount: '', overpay: false, pending: false, retryReady: false, blocked: false, finished: false })
Page({
  data: initial(),
  onLoad(query) { this.query = query || {}; this.sequence = 0 },
  onShow() { this.refresh() },
  onHide() { this.clearPrivate() },
  onUnload() { this.clearPrivate() },
  onPullDownRefresh() { if (!this.data.busy && !this.data.opening) this.refresh().finally(() => wx.stopPullDownRefresh()); else wx.stopPullDownRefresh() },
  clearPrivate() { this.sequence = (this.sequence || 0) + 1; this.review = null; this.setData(initial()) },
  context() { const seq = this.sequence, auth = session.snapshot(), scope = landlord.snapshot(); return () => seq === this.sequence && session.matches(auth) && landlord.matches(scope) },
  fail(e, field = 'formError') {
    if (e.status === 401 && !e.stale && !session.token()) { goLogin(); return }
    if (!e.stale) { if (e.status === 403) this.setData({ canEdit: false }); this.setData({ [field]: e.message || '读取失败，请重试' }) }
  },
  applyDetail(data) { this.setData({ bill: fmt.bill(data.bill), payments: data.payments.map(fmt.payment) }) },
  async refresh() {
    if (this.data.busy || this.data.opening) return
    this.clearPrivate(); const seq = this.sequence, auth = session.snapshot(), guardCurrent = () => seq === this.sequence && session.matches(auth)
    this.setData({ loading: true })
    try {
      const { ledger, ledgers, user } = await landlord.guard(false, guardCurrent)
      if (!guardCurrent()) return
      const current = this.context()
      this.setData({ username: user.username, ledgers, ledgerIndex: Math.max(0, ledgers.findIndex(v => ledger && v.id === ledger.id)), ledgerName: ledger ? ledger.name : '', canEdit: landlord.canEdit(ledger, 'bills') })
      if (!landlord.canView(ledger, 'bills')) throw Error('当前台账没有账单查看权限')
      if (!/^[1-9]\d*$/.test(this.query.id || '')) throw Error('请选择有效账单')
      const data = await landlord.request('/rental/bills/' + this.query.id, { isCurrent: current })
      if (!current()) return
      this.applyDetail(data)
      const p = intent.load()
      if (p && (p.ledgerID !== ledger.id || p.billID !== Number(this.query.id))) { this.setData({ blocked: true, message: '还有一笔实收待核对，请回到原账单完成核对。' }); return }
      if (p) { this.setData({ pending: true, amount: String(p.payload.amount), note: p.payload.note }); await this.reconcile(current) }
    } catch (e) { if (guardCurrent() || (e.status === 401 && !e.stale && !session.token())) this.fail(e, 'error') }
    finally { if (seq === this.sequence) this.setData({ loading: false }) }
  },
  input(e) {
    if (this.data.busy || this.data.pending || this.data.blocked || this.data.finished || !this.data.canEdit) return
    const field = e.currentTarget.dataset.field
    if (!['amount', 'note'].includes(field)) return
    this.review = null; this.setData({ [field]: e.detail.value, reviewed: false, formError: '', message: '' })
  },
  async reviewPayment() {
    if (this.data.busy || this.data.pending || this.data.blocked || this.data.finished || !this.data.canEdit) return
    let money, note
    try { money = intent.amount(this.data.amount); note = this.data.note.trim(); if (Array.from(note).length > 500) throw Error('备注最多500字') } catch (e) { this.fail(e); return }
    const current = this.context(); this.setData({ busy: true, formError: '', reviewed: false }); this.review = null
    try {
      const detail = await landlord.request('/rental/bills/' + this.query.id, { isCurrent: current })
      if (!current()) return
      this.applyDetail(detail); this.review = { amount: money, note, expected: intent.expected(detail.bill) }
      this.setData({ reviewed: true, reviewedAmount: fmt.money(money), overpay: money + Number(detail.bill.paid_amount || 0) > Number(detail.bill.payable || 0) })
    } catch (e) { this.fail(e) }
    finally { if (current()) this.setData({ busy: false }) }
  },
  async submit() {
    if (this.data.busy || !this.data.canEdit || !this.data.reviewed || !this.review || this.data.pending || this.data.blocked || this.data.finished) return
    let saved
    try { saved = intent.save(landlord.snapshot().id, Number(this.query.id), this.review.amount, this.review.note) } catch (e) { this.fail(e); return }
    this.setData({ pending: true, retryReady: false, reviewed: false })
    return this.send(saved, this.review.expected)
  },
  async send(saved, expected) {
    const current = this.context(); this.setData({ busy: true, formError: '', message: '', retryReady: false })
    try {
      await landlord.request('/rental/bills/' + this.query.id + '/pay', { method: 'POST', data: Object.assign({}, saved.payload, { expected }), isCurrent: current })
      if (current()) await this.reconcile(current)
    } catch (e) {
      if (current()) { this.setData({ pending: true, retryReady: false, formError: e.status === 409 ? '账单已有变化，请核对服务器记录后再确认原请求。' : '登记结果尚未确认，请先核对本笔实收，勿重新登记。' }); if (e.status === 401 || e.status === 403) this.fail(e) }
      else if (e.status === 401 && !e.stale && !session.token()) this.fail(e)
    } finally { if (current()) this.setData({ busy: false }) }
  },
  async reconcile(current) {
    const saved = intent.load()
    if (!saved || saved.billID !== Number(this.query.id) || saved.ledgerID !== landlord.snapshot().id) return
    const result = await landlord.request('/rental/bills/' + this.query.id + '/payment-status', { data: { request_key: saved.payload.request_key }, isCurrent: current })
    if (!current()) return
    this.setData({ bill: fmt.bill(result.bill), formError: '' })
    if (result.found) {
      const p = result.payment
      if (!p || p.bill_id !== saved.billID || Number(p.amount) !== saved.payload.amount || p.note !== saved.payload.note || !p.receipt_id) throw Error('原请求对应的实收资料不一致，请保留待核对记录并在Web核对')
      intent.clear(saved.payload.request_key); this.review = null
      this.setData({ pending: false, retryReady: false, finished: true, amount: '', note: '', payments: [fmt.payment(p)], message: p.voided_at ? '已找到原实收，现已冲销；请核对原收据及Web流水。' : '已核对本笔实收与归档收据。' })
    } else this.setData({ pending: true, retryReady: true, message: '尚未查到本笔实收。可能仍在处理；重试将使用原金额、备注和请求编号，请核对后明确确认。', overpay: saved.payload.amount + Number(result.bill.paid_amount || 0) > Number(result.bill.payable || 0), reviewedAmount: fmt.money(saved.payload.amount) })
  },
  async checkPayment() {
    if (this.data.busy || !this.data.pending) return
    const current = this.context(); this.setData({ busy: true, retryReady: false, formError: '' })
    try { await this.reconcile(current) } catch (e) { if (current() || (e.status === 401 && !e.stale && !session.token())) this.fail(e) }
    finally { if (current()) this.setData({ busy: false }) }
  },
  retry() { if (!this.data.busy && this.data.canEdit && this.data.pending && this.data.retryReady) { const saved = intent.load(); if (saved && saved.billID === Number(this.query.id) && saved.ledgerID === landlord.snapshot().id) return this.send(saved, intent.expected(this.data.bill)) } },
  newPayment() { if (!this.data.busy && !this.data.pending) return this.refresh() },
  async openReceipt(e) {
    if (this.data.busy || this.data.opening) return
    const id = Number(e.currentTarget.dataset.id)
    if (!this.data.payments.some(p => p.receipt_id === id)) return
    const current = this.context(); this.setData({ opening: true })
    try { await files.openReceipt(id, landlord.snapshot().id, current) } catch (e) { if (current() || (e.status === 401 && !e.stale && !session.token())) this.fail(e) }
    finally { if (current()) this.setData({ opening: false }) }
  },
  changeLedger() { if (!this.data.busy && !this.data.opening) wx.reLaunch({ url: '/pages/landlord/home/index' }) },
  goHome() { if (!this.data.busy && !this.data.opening) wx.reLaunch({ url: '/pages/landlord/home/index' }) },
  logout() { if (!this.data.busy && !this.data.opening) logout() },
})
