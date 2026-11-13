const landlord = require('../../../lib/landlord')
const fmt = require('../../../lib/format')
landlord.landlordPage({
  module: 'rooms', initial: () => ({ items: [], total: 0, page: 1, pages: 1, keyword: '' }),
  async load() {
    const page = this.targetPage || 1
    const data = await landlord.request('/rental/rooms', { data: { page, pageSize: 10, keyword: this.keyword || '' } })
    return { items: data.items.map(row => ({ id: row.id, room_no: row.room_no, community: row.community, building: row.building, address: row.address, rentText: fmt.money(row.default_rent), occupied: (row.current_tenants || []).length > 0 })), total: data.total, page, pages: Math.max(1, Math.ceil(data.total / 10)), keyword: this.keyword || '' }
  },
  methods: {
    inputKeyword(e) { this.keyword = e.detail.value; this.setData({ keyword: this.keyword }) },
    search() { this.targetPage = 1; this.refresh() },
    previous() { this.targetPage = Math.max(1, this.data.page - 1); this.refresh() },
    next() { this.targetPage = Math.min(this.data.pages, this.data.page + 1); this.refresh() },
  },
})
