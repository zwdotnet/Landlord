const titles = { login: '', dashboard: '房东工作台', meters: '抄表', bill: '账单详情', waiting: '入住登记', profile: '我的' }
Page({
  data: {
    community: '\u9752\u79be\u91cc', residentName: '\u9648\u5609\u5b81',
    kind: 'login', title: '', role: 'landlord', sheet: '',
    phone: '138' + '0000' + '9024', phoneDisplay: ['138', '0000', '9024'].join(' '),
    newPhone: '', phoneError: '', meterSaved: false,
    meters: [
      { key: 'water', symbol: '水', name: '水表', unit: 'm³', last: '120.0', value: '128.6', used: '8.6', tone: 'water' },
      { key: 'elec', symbol: '电', name: '电表', unit: 'kWh', last: '856.0', value: '981.5', used: '125.5', tone: 'power' },
      { key: 'gas', symbol: '气', name: '燃气表', unit: 'm³', last: '68.0', value: '68.0', used: '0.0', tone: 'gas' },
    ],
    fees: [{ name: '房租', detail: '11.01 — 11.30', value: '2,200.00' }, { name: '水费', detail: '8.6 m³ × ¥5.00', value: '43.00' }, { name: '电费', detail: '125.5 kWh × ¥1.00', value: '125.50' }],
    shortcuts: [
      { icon: 'dashboard', label: '抄表录入', note: '水电气用量', target: 'meters' },
      { icon: 'file-add', label: '预览出账', note: '核对后出账', target: 'bill' },
      { icon: 'wallet', label: '登记实收', note: '流水与收据', target: 'bill', sheet: 'payment' },
      { icon: 'home', label: '房源管理', note: '房源与入住', target: 'dashboard' },
    ],
  },
  onLoad(query) {
    const kind = titles[query.kind] === undefined ? 'login' : query.kind
    this.setData({ kind, title: titles[kind], sheet: query.sheet || '' })
  },
  go(e) {
    const { target, sheet } = e.currentTarget.dataset
    if (!titles[target] && target !== 'login') return
    wx.navigateTo({ url: '/pages/preview/index?kind=' + target + (sheet ? '&sheet=' + sheet : '') })
  },
  back() { wx.navigateBack({ fail: () => wx.reLaunch({ url: '/pages/preview/index?kind=dashboard' }) }) },
  switchRole(e) { this.setData({ role: e.currentTarget.dataset.role }) },
  openPhone() { this.setData({ sheet: 'phone', newPhone: this.data.phone, phoneError: '' }) },
  closeSheet() { this.setData({ sheet: '', phoneError: '' }) },
  sheetChange(e) { if (!e.detail.visible) this.closeSheet() },
  inputPhone(e) { this.setData({ newPhone: e.detail.value, phoneError: '' }) },
  savePhone() {
    const phone = this.data.newPhone.trim()
    if (!/^1[3-9]\d{9}$/.test(phone)) { this.setData({ phoneError: '请输入正确的 11 位手机号' }); return }
    this.setData({ phone, phoneDisplay: phone.replace(/(\d{3})(\d{4})(\d{4})/, '$1 $2 $3'), sheet: '' })
  },
  openPayment() { this.setData({ sheet: 'payment' }) },
  previewSuccess() { this.setData({ sheet: 'success' }) },
  saveMeters() { this.setData({ meterSaved: true }) },
})
