// Unit tests use a synthetic base even when a developer has local credentials.
// Reinstall after each test's module-cache reset; never create config.local.js.
const path = require('node:path')
module.exports = function testConfig(root = path.resolve(__dirname, '../../miniprogram')) {
  const filename = path.join(root, 'config.js')
  require.cache[filename] = {
    id: filename, filename, loaded: true,
    exports: Object.freeze({ apiBase: 'https://landlord-test.invalid/api' }),
  }
}
