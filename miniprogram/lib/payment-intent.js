const session = require('./session')
const KEY = ['landlord', 'payment', 'pending', 'v1'].join('.')
let lastToken = session.token()
session.subscribe(() => { if (lastToken !== session.token()) { wx.removeStorageSync(KEY); lastToken = session.token() } })
function load() {
  const value = wx.getStorageSync(KEY), user = session.current()
  if (!value) return null
  if (!user || value.userID !== user.user.id || value.version !== 1 || !Number.isInteger(value.ledgerID) || value.ledgerID <= 0 || !Number.isInteger(value.billID) || value.billID <= 0 || !value.payload || !/^mp-[a-z0-9-]{20,80}$/.test(value.payload.request_key) || !Number.isFinite(value.payload.amount) || value.payload.amount <= 0 || typeof value.payload.note !== 'string' || Array.from(value.payload.note).length > 500) {
    wx.removeStorageSync(KEY); return null
  }
  return value
}
function amount(text) {
  if (!/^\d+(\.\d{1,2})?$/.test(String(text).trim())) throw Error('请输入大于0、最多两位小数的金额')
  const value = Number(text)
  if (!Number.isFinite(value) || value <= 0 || value > 100000000) throw Error('请核对收款金额，单笔不得超过1亿元')
  return Math.round(value * 100) / 100
}
function save(ledgerID, billID, money, note) {
  if (load()) throw Error('还有一笔实收待核对，请先完成核对')
  const user = session.current()
  if (!user) throw Error('请先登录')
  const key = 'mp-' + Date.now().toString(36) + '-' + Array.from({ length: 4 }, () => Math.random().toString(36).slice(2, 10)).join('')
  const value = { version: 1, userID: user.user.id, ledgerID, billID, payload: { request_key: key, amount: money, note } }
  wx.setStorageSync(KEY, value)
  if (JSON.stringify(wx.getStorageSync(KEY)) !== JSON.stringify(value)) throw Error('无法保存待核对记录，本次尚未发送')
  return value
}
function clear(key) { const v = load(); if (v && v.payload.request_key === key) wx.removeStorageSync(KEY) }
function expected(bill) { return { updated_at: bill.updated_at, paid_amount: bill.paid_amount, payable: bill.payable } }
module.exports = { load, save, clear, amount, expected }
