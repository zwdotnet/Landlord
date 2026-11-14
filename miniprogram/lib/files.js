const session = require('./session')
const { url, error } = require('./request')
const KEY = ['landlord', 'tenant', 'files', 'v1'].join('.')
function remove(path) {
  if (!path) return
  try { wx.getFileSystemManager().unlinkSync(path) } catch (_) { /* Retry tracked files on next launch. */ }
}
function clean() {
  const paths = wx.getStorageSync(KEY) || []
  const failed = []
  paths.forEach(path => {
    try { wx.getFileSystemManager().unlinkSync(path) }
    catch (_) {
      try { wx.getFileSystemManager().accessSync(path); failed.push(path) } catch (_) { /* Already absent. */ }
    }
  })
  if (failed.length) wx.setStorageSync(KEY, failed)
  else wx.removeStorageSync(KEY)
}
function track(path) { const paths = wx.getStorageSync(KEY) || []; if (!paths.includes(path)) wx.setStorageSync(KEY, paths.concat(path)) }
function classify(buffer) {
  const b = new Uint8Array(buffer)
  if (b.length >= 5 && String.fromCharCode(...b.slice(0, 5)) === '%PDF-') return 'pdf'
  if (b.length >= 8 && b[0] === 137 && b[1] === 80 && b[2] === 78 && b[3] === 71) return 'image'
  if (b.length >= 3 && b[0] === 255 && b[1] === 216 && b[2] === 255) return 'image'
  if (b.length >= 6 && String.fromCharCode(...b.slice(0, 3)) === 'GIF') return 'image'
  if (b.length >= 12 && String.fromCharCode(...b.slice(0, 4)) === 'RIFF' && String.fromCharCode(...b.slice(8, 12)) === 'WEBP') return 'image'
  return ''
}
async function open(path, expected = 'pdf') {
  if (!/^\/rental\/portal\/(documents|contracts)\/[1-9]\d*\/file$/.test(path)) throw error('无效的文件路径')
  return download(path, expected)
}
async function openReceipt(id, ledgerID, isCurrent) {
  if (!/^[1-9]\d*$/.test(String(id)) || !/^[1-9]\d*$/.test(String(ledgerID)) || typeof isCurrent !== 'function') throw error('无效的收据或台账')
  return download('/rental/documents/' + id + '/file?ledger_id=' + ledgerID, 'pdf', isCurrent)
}
async function download(path, expected, scopeCurrent = () => true) {
  const state = session.snapshot()
  if (!state.token) throw error('请先登录', 401)
  const current = () => session.matches(state) && scopeCurrent()
  if (!current()) throw error('页面或台账已切换', 0, true)
  clean()
  // Native download temp paths can be read-only (observed http://tmp in DevTools).
  // Own the output under USER_DATA_PATH so logout can actually unlink it.
  const destination = wx.env.USER_DATA_PATH + '/landlord-doc-' + Date.now() + '-' + Math.random().toString(16).slice(2) + (expected === 'pdf' ? '.pdf' : '.img')
  track(destination)
  const file = await new Promise((resolve, reject) => {
    wx.downloadFile({ url: url(path), filePath: destination, header: { Authorization: 'Bearer ' + state.token }, timeout: 25000,
      success(res) {
        const name = res.filePath || res.tempFilePath
        if (name) track(name)
        if (!current()) { remove(name); reject(error('页面、账号或台账已切换', 0, true)); return }
        if (res.statusCode !== 200 || !name) {
          remove(name)
          if (res.statusCode === 401) session.clear()
          reject(error(res.statusCode === 401 ? '登录已过期，请重新登录' : '文件无法读取，请刷新后重试', res.statusCode)); return
        }
        resolve(name)
      }, fail() { remove(destination); reject(error(current() ? '下载失败，请检查网络后重试' : '页面或台账已切换', 0, !current())) },
    })
  })
  try {
    const kind = await new Promise((resolve, reject) => {
      wx.getFileSystemManager().readFile({ filePath: file,
        success(res) { resolve(classify(res.data)) }, fail() { reject(error('文件读取失败')) } })
    })
    if (!current()) throw error('页面、账号或台账已切换', 0, true)
    if (!kind || (expected === 'pdf' && kind !== 'pdf')) throw error('返回的内容不是有效文件，请刷新后重试')
    await new Promise((resolve, reject) => {
      const callbacks = { success: resolve, fail: () => reject(error('无法打开文件，请稍后重试')) }
      if (kind === 'pdf') wx.openDocument(Object.assign({ filePath: file, fileType: 'pdf', showMenu: true }, callbacks))
      else wx.previewImage(Object.assign({ current: file, urls: [file] }, callbacks))
    })
  } catch (e) { remove(file); throw e }
}
module.exports = { open, openReceipt, clean, classify }
