const { tenantPage } = require('../../lib/page')
tenantPage({ pending: true, initial: () => ({ phone: null, accountId: 0 }), load: async status => ({ phone: status.phone || '', accountId: status.account_id }) })
