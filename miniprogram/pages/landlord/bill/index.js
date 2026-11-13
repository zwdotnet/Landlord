const landlord = require('../../../lib/landlord')
const { error } = require('../../../lib/request')
const fmt = require('../../../lib/format')
landlord.landlordPage({
  module: 'bills', initial: () => ({ bill: null, items: [], payments: [] }),
  async load() {
    if (!/^[1-9]\d*$/.test(this.query.id || '')) throw error('账单不存在', 404)
    const data = await landlord.request('/rental/bills/' + this.query.id)
    return { bill: fmt.bill(data.bill), items: data.items.map(row => Object.assign({}, row, { amountText: fmt.money(row.amount) })), payments: data.payments.map(fmt.payment) }
  },
})
