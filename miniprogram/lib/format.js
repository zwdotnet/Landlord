function money(value) { const n = Number(value); return (Number.isFinite(n) ? n : 0).toFixed(2) }
function date(value) { return value ? String(value).slice(0, 10) : '未填写' }
function time(value) {
  if (!value) return '未填写'
  const ms = Date.parse(value)
  if (!Number.isFinite(ms)) return String(value).slice(0, 16).replace('T', ' ')
  // Date-only strings remain dates. Other timestamps are shown in the business timezone.
  if (String(value).length === 10) return String(value)
  return new Date(ms + 8 * 3600000).toISOString().slice(0, 16).replace('T', ' ')
}
function bill(row) {
  const payable = row.payable == null ? Number(row.total_amount || 0) + Number(row.carry_in || 0) : Number(row.payable)
  return Object.assign({}, row, { payableText: money(payable), totalText: money(row.total_amount),
    paidText: money(row.paid_amount), carryText: money(row.carry_in),
    dueText: money(Math.max(0, payable - Number(row.paid_amount || 0))),
    statusText: ({ paid: '已缴清', partial: '部分缴费', unpaid: '待缴费' })[row.status] || '待核对' })
}
function payment(row) {
  return Object.assign({}, row, { amountText: money(Math.abs(Number(row.amount))), dateText: time(row.paid_at),
    kindText: row.reversal_of_id || Number(row.amount) < 0 ? '冲销' : '实收',
    stateText: row.voided_at ? '已冲销' : (row.reversal_of_id || Number(row.amount) < 0 ? '冲销凭证' : '原始收据') })
}
module.exports = { money, date, time, bill, payment }
