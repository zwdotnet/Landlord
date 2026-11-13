const { landlordPage } = require('../../../lib/landlord')
landlordPage({ pending: true, initial: () => ({}), load: async () => ({}) })
