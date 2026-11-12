const { tenantPage } = require('../../lib/page')
const { request } = require('../../lib/request')
const fmt = require('../../lib/format')
tenantPage({
  initial: () => ({ agreements: [], contracts: [] }),
  async load() {
    const [agreements, contracts] = await Promise.all([request('/rental/portal/agreements'), request('/rental/portal/contracts')])
    return { agreements: agreements.map(d => Object.assign({}, d, { title: d.kind === 'renewal' ? '续租协议' : '租赁合同', dateText: fmt.date(d.created_at) })), contracts: contracts.map(d => Object.assign({}, d, { dateText: fmt.date(d.created_at), kind: d.mime_type === 'application/pdf' ? 'pdf' : 'image' })) }
  },
})
