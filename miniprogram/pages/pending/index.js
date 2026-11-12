const { tenantPage } = require('../../lib/page')
tenantPage({ pending: true, initial: () => ({ username: '' }), load: async status => ({ username: status.username }) })
