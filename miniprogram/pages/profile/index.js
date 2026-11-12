const { tenantPage } = require('../../lib/page')
const { request } = require('../../lib/request')
const fmt = require('../../lib/format')
tenantPage({ initial: () => ({ profile: null, username: '' }),
  async load(status) {
    const data = await request('/rental/portal/profile')
    return { username: status.username, profile: Object.assign({}, data.profile, { depositText: fmt.money(data.profile.deposit) }) }
  },
})
