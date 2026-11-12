const { tenantPage } = require('../../lib/page')
const { request } = require('../../lib/request')
const fmt = require('../../lib/format')
tenantPage({
  initial: () => ({ items: [], page: 1, pages: 1, total: 0 }),
  async load() {
    const page = this.targetPage || 1
    const data = await request('/rental/portal/bills', { data: { page, pageSize: 10 } })
    return { items: data.items.map(fmt.bill), total: data.total, page, pages: Math.max(1, Math.ceil(data.total / 10)) }
  },
  methods: {
    previous() { this.targetPage = Math.max(1, this.data.page - 1); this.refresh() },
    next() { this.targetPage = Math.min(this.data.pages, this.data.page + 1); this.refresh() },
  },
})
