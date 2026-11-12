const { tenantPage } = require('../../lib/page')
const { request } = require('../../lib/request')
const fmt = require('../../lib/format')
tenantPage({
  initial: () => ({ profile: null, latest: null }),
  async load() {
    const [payload, bills] = await Promise.all([request('/rental/portal/profile'), request('/rental/portal/bills', { data: { page: 1, pageSize: 1 } })])
    const p = payload.profile
    return { profile: Object.assign({}, p, { moveInText: fmt.date(p.move_in_date), endText: fmt.date(p.lease_end_date) }), latest: bills.items.length ? fmt.bill(bills.items[0]) : null }
  },
})
