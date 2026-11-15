const { tenantPage } = require('../../lib/page')
const { request } = require('../../lib/request')
const fmt = require('../../lib/format')
tenantPage({ initial: () => ({ profile: null, phone: '', accountId: 0 }),
  async load(status) {
    const data = await request('/rental/portal/profile')
    return { phone: status.phone || '', accountId: status.account_id, profile: Object.assign({}, data.profile, { depositText: fmt.money(data.profile.deposit), endText: fmt.date(data.profile.lease_end_date) }) }
  },
})
