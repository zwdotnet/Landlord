const { tenantPage } = require('../../lib/page')
const { request, error } = require('../../lib/request')
const fmt = require('../../lib/format')
tenantPage({
  initial: () => ({ bill: null, items: [], payments: [], property: null }),
  async load() {
    if (!/^[1-9]\d*$/.test(this.query.id || '')) throw error('账单不存在', 404)
    const data = await request('/rental/portal/bills/' + this.query.id)
    return { bill: fmt.bill(data.bill), items: data.items.map(row => Object.assign({}, row, { amountText: fmt.money(row.amount), paidText: fmt.money(row.paid) })), payments: data.payments.map(fmt.payment), property: data.property }
  },
})
