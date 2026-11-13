import assert from 'node:assert/strict'
import { test } from 'node:test'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import ts from 'typescript'

function subscriptionReader(notification) {
 const exports = {}
 const source = ts.transpileModule(readFileSync(new URL('../src/utils/push-subscription.ts', import.meta.url), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
 vm.runInNewContext(source, { exports, ...(notification ? { Notification: notification } : {}) })
 return exports.existingPushSubscription
}

test('ungranted notification permission never touches the native push service', async () => {
 const registration = { get pushManager() { throw new Error('native push service was touched') } }
 for (const permission of ['default', 'denied']) {
  assert.equal(await subscriptionReader({ permission })(registration), null)
 }
 assert.equal(await subscriptionReader()(registration), null)
})
test('granted permission preserves the existing subscription for account binding and removal', async () => {
 const subscription = { endpoint: 'https://push.example/synthetic-device' }
 let calls = 0
 const read = subscriptionReader({ permission: 'granted' })
 assert.equal(await read({ pushManager: { async getSubscription() { calls++; return subscription } } }), subscription)
 assert.equal(calls, 1)
 assert.equal(await read({ pushManager: { async getSubscription() { return null } } }), null)
 assert.equal(await read(null), null)
 assert.equal(await read({}), null)
})
test('native subscription failures remain visible to the calling UI', async () => {
 const read = subscriptionReader({ permission: 'granted' })
 await assert.rejects(read({ pushManager: { async getSubscription() { throw new Error('push unavailable') } } }), /push unavailable/)
})
function worker() {
 const events={}, stores=new Map(), shown=[], opened=[]
 const scope='https://www.zoeng.site/landlord/'
 const caches={async open(name){if(!stores.has(name))stores.set(name,new Map());const m=stores.get(name);return {put:async(k,v)=>m.set(typeof k==='string'?k:k.url,v),match:async k=>m.get(typeof k==='string'?k:k.url),addAll:async()=>{}}},async keys(){return [...stores.keys()]},delete:async name=>stores.delete(name),match:async()=>new Response('offline')}
 const self={registration:{scope,showNotification:async(...args)=>shown.push(args)},location:{origin:'https://www.zoeng.site'},clients:{matchAll:async()=>[],openWindow:async url=>opened.push(url),claim:async()=>{}},addEventListener:(type,fn)=>events[type]=fn,skipWaiting:()=>{}}
 vm.runInNewContext(readFileSync(new URL('../public/sw.js',import.meta.url),'utf8'),{self,caches,Response,URL,fetch:async()=>{throw new Error('offline')}})
 async function emit(type,data){const waits=[];events[type]({...data,waitUntil:p=>waits.push(p)});await Promise.all(waits)}
 return {events,shown,opened,emit}
}
test('push shows only for the account explicitly bound to this worker',async()=>{
 const w=worker();const push={data:{json:()=>({title:'租务提醒',body:'新消息',user_id:7,tag:'notice-1'})}}
 await w.emit('push',push);assert.equal(w.shown.length,0)
 await w.emit('message',{data:{type:'SET_PUSH_USER',userId:7}});await w.emit('push',push);assert.equal(w.shown.length,1)
 await w.emit('message',{data:{type:'SET_PUSH_USER',userId:8}});await w.emit('push',push);assert.equal(w.shown.length,1)
 await w.emit('message',{data:{type:'SET_PUSH_USER',userId:null}});await w.emit('push',push);assert.equal(w.shown.length,1)
})
test('notification navigation stays within the installed application scope',async()=>{
 const w=worker();await w.emit('notificationclick',{notification:{close(){},data:{path:'https://evil.example/'}}});assert.equal(w.opened.length,0)
 await w.emit('notificationclick',{notification:{close(){},data:{path:'/portal/bills'}}});assert.deepEqual(w.opened,['https://www.zoeng.site/landlord/portal/bills'])
 await w.emit('notificationclick',{notification:{close(){},data:{path:'../admin/'}}});assert.equal(w.opened.length,1)
})
test('the service worker leaves authenticated APIs and private files uncached',()=>{
 const w=worker();for(const path of ['api/rental/bills','api/rental/documents/1/file','uploads/private.pdf']){
  let handled=false;w.events.fetch({request:{method:'GET',url:'https://www.zoeng.site/landlord/'+path,mode:'cors'},respondWith(){handled=true}});assert.equal(handled,false)
 }
})
