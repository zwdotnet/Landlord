function loginCode() {
  return new Promise((resolve, reject) => wx.login({
    timeout: 10000,
    success: result => result.code ? resolve(result.code) : reject(new Error('未取得微信登录凭证，请重试')),
    fail: () => reject(new Error('微信登录未完成，请重试')),
  }))
}
module.exports = { loginCode }
