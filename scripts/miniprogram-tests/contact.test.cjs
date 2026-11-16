const test = require('node:test'), assert = require('node:assert/strict'), path = require('node:path')
const root = path.resolve(__dirname, '../../miniprogram')
const tick = () => new Promise(r => setImmediate(r))
function env() {
  for (const k of Object.keys(require.cache)) if (k.startsWith(root + path.sep)) delete require.cache[k]
  require('./test-config.cjs')(root)
  const requests=[], storage=new Map(); let page
  global.wx={getStorageSync:k=>storage.get(k),setStorageSync:(k,v)=>storage.set(k,v),removeStorageSync:k=>storage.delete(k),request:o=>requests.push(o),reLaunch(){},switchTab(){},navigateTo(){}}
  global.getCurrentPages=()=>[page]
  global.Page=o=>{page=o;page.setData=x=>Object.assign(page.data,x)}
  const session=require(root+'/lib/session');session.start({token:'synthetic',user:{id:1,role:'tenant',username:'wx_internal'}})
  require(root+'/pages/contact/index');page.onLoad()
  return {page,requests,session}
}
function reply(r,data,statusCode=200){r.success({statusCode,data:{code:statusCode===200?0:statusCode,data,message:'测试拒绝'}})}
async function load(e){const p=e.page.refresh();reply(e.requests.shift(),{role:'tenant'});await tick();reply(e.requests.shift(),{phone:'13800009001'});await p}
test('pending contact edit validates input, guards duplicate taps and sends only own expected value',async()=>{
  const e=env();await load(e);const p=e.page
  p.input({detail:{value:'wrong'}});await p.save();assert.equal(e.requests.length,0)
  p.input({detail:{value:'13800009002'}});const saving=p.save();p.save();assert.equal(e.requests.length,1)
  const r=e.requests.shift();assert.deepEqual(r.data,{phone:'13800009002',expected_phone:'13800009001'})
  reply(r,{phone:'13800009002'});await saving;assert.equal(p.data.original,'13800009002');assert.equal(p.data.busy,false)
})
test('lost or conflicting contact response blocks resending until explicit readback',async()=>{
 for(const status of [0,409,500]){
  const e=env();await load(e);e.page.input({detail:{value:'13800009002'}});const saving=e.page.save(),r=e.requests.shift()
  if(status===0)r.fail();else reply(r,null,status)
  await saving;assert.equal(e.page.data.needsCheck,true);await e.page.save();assert.equal(e.requests.length,0)
  await load(e);assert.equal(e.page.data.needsCheck,false);assert.equal(e.page.data.phone,'13800009001')
 }
})
test('hidden contact response cannot repopulate phone or clear replacement account',async()=>{
 const e=env();await load(e);e.page.input({detail:{value:'13800009002'}});const saving=e.page.save(),r=e.requests.shift()
 e.page.onHide();e.session.start({token:'new',user:{id:2,role:'tenant',username:'other'}});reply(r,null,401);await saving
 assert.equal(e.page.data.phone,'');assert.equal(e.session.token(),'new')
})
