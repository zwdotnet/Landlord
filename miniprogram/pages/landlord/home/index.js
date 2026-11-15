const landlord = require('../../../lib/landlord')
const fmt = require('../../../lib/format')
landlord.landlordPage({
  initial: () => ({ roomTotal: 0, billTotal: 0, latest: null }),
  async load({ ledger }) {
    const [rooms, bills] = await Promise.all([
      landlord.canView(ledger, 'rooms') ? landlord.request('/rental/rooms', { data: { page: 1, pageSize: 1 } }) : null,
      landlord.canView(ledger, 'bills') ? landlord.request('/rental/bills', { data: { page: 1, pageSize: 1 } }) : null,
    ])
    return { roomTotal: rooms ? rooms.total : 0, billTotal: bills ? bills.total : 0, latest: bills && bills.items.length ? fmt.bill(bills.items[0]) : null }
  },
})
