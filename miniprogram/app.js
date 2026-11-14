const session = require('./lib/session')
const files = require('./lib/files')
require('./lib/payment-intent')

App({
  onLaunch() {
    files.clean()
    session.subscribe(() => {
      files.clean()
      const pages = getCurrentPages()
      pages.forEach(page => { if (page.clearPrivate) page.clearPrivate() })
    })
  },
})
