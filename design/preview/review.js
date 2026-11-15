const screens = [
  {key:'login',title:'登录',role:'共用入口',caption:'先选择租客或房东。账号登录与微信入口各有层级，留白集中在欢迎区。'},
  {key:'dashboard',title:'房东工作台',role:'房东',caption:'一个待收金额，四个高频操作。待办与近期账单分组，避免整屏按钮堆叠。'},
  {key:'meters',title:'本月抄表',role:'房东',caption:'房源、上期读数、本次用量分别成层。保存按钮固定在底部，便于单手操作。'},
  {key:'bill',title:'账单与实收',role:'房东',caption:'费用清楚展开，待收金额保持突出。实收采用底部弹层，先核对金额再确认。',variant:'payment',variantLabel:'查看实收弹层'},
  {key:'waiting',title:'等待关联',role:'租客',caption:'直接展示登记手机号。填错可就地修改，同时说明房东关联的进度。',variant:'phone',variantLabel:'查看手机号修改'},
  {key:'profile',title:'我的',role:'租客',caption:'手机号作为账号展示，租住信息与业务入口分组，修改入口长期可见。',variant:'phone',variantLabel:'查看手机号修改'},
]
const grid=document.querySelector('#screens')
const dialog=document.querySelector('.rv-dialog')
const nav=document.querySelector('.rv-screen-nav')
let view='grid', selected=0, opened=0, variant=''
let lastTrigger=null
function render(){
  const visible=view==='grid'?screens:[screens[selected]]
  grid.classList.toggle('is-single',view==='single')
  grid.innerHTML=visible.map(item=>`<article class="rv-card" id="${item.key}"><div class="rv-card-heading"><h2>${item.title}</h2><span class="rv-role">${item.role}</span></div><button class="rv-phone" type="button" data-open="${item.key}" aria-label="放大查看${item.title}"><img src="assets/${item.key}.png" alt="${item.title}的 TDesign 小程序真实组件截图" width="359" height="777"><span class="rv-zoom">点击查看大图</span></button><p class="rv-card-caption">${item.caption}</p>${item.variant?`<button class="rv-state-link" type="button" data-open="${item.key}" data-variant="${item.variant}">${item.variantLabel}<span aria-hidden="true">↗</span></button>`:''}</article>`).join('')
  nav.hidden=view!=='single'
  nav.innerHTML=screens.map((item,i)=>`<button type="button" data-screen="${i}" class="${selected===i?'is-active':''}" aria-pressed="${selected===i}">${item.title}</button>`).join('')
  document.querySelector('#view-note').textContent=view==='grid'?'点开任一页面，查看大图和弹层方案':'左右切换页面，点击截图查看大图'
}
function renderDialog(){
  const screen=screens[opened]
  document.querySelector('#dialog-title').textContent=screen.title
  document.querySelector('.rv-dialog-role').textContent=screen.role
  const img=document.querySelector('.rv-large-image')
  img.src=`assets/${screen.key}${variant?'-'+variant:''}.png`
  img.alt=screen.title+(variant==='phone'?' · 手机号修改弹层':variant==='payment'?' · 实收登记弹层':'')
  document.querySelector('.rv-dialog-caption').textContent=screen.caption
  const states=[['','页面预览'],...(screen.variant?[[screen.variant,screen.variant==='phone'?'修改手机号':'登记实收']]:[])]
  document.querySelector('.rv-variants').innerHTML=states.map(([key,label])=>`<button type="button" data-state="${key}" class="${variant===key?'is-active':''}" aria-pressed="${variant===key}">${label}</button>`).join('')
}
document.addEventListener('click',e=>{
  const opener=e.target.closest('[data-open]')
  if(opener){lastTrigger=opener;opened=screens.findIndex(x=>x.key===opener.dataset.open);variant=opener.dataset.variant||'';renderDialog();dialog.showModal();return}
  const toggle=e.target.closest('[data-view]')
  if(toggle){view=toggle.dataset.view;document.querySelectorAll('[data-view]').forEach(x=>{x.classList.toggle('is-active',x===toggle);x.setAttribute('aria-pressed',String(x===toggle))});render();return}
  const screen=e.target.closest('[data-screen]')
  if(screen){selected=Number(screen.dataset.screen);render();return}
  const state=e.target.closest('[data-state]')
  if(state){variant=state.dataset.state;renderDialog()}
})
function close(){dialog.close();if(lastTrigger&&lastTrigger.isConnected)lastTrigger.focus()}
document.querySelector('.rv-close').addEventListener('click',close)
dialog.addEventListener('click',e=>{if(e.target===dialog)close()})
dialog.addEventListener('cancel',()=>{if(lastTrigger&&lastTrigger.isConnected)lastTrigger.focus()})
function step(n){opened=(opened+n+screens.length)%screens.length;variant='';renderDialog()}
document.querySelector('.rv-previous').addEventListener('click',()=>step(-1))
document.querySelector('.rv-next').addEventListener('click',()=>step(1))
document.addEventListener('keydown',e=>{if(!dialog.open)return;if(e.key==='ArrowLeft')step(-1);if(e.key==='ArrowRight')step(1)})
render()
