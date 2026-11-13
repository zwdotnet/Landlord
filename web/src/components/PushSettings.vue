<template>
  <section class="surface rounded-2xl p-4 sm:p-6 space-y-3">
    <h2 class="text-lg font-semibold">安装与设备通知</h2>
    <p class="text-sm text-muted-foreground">{{ guidance }}</p>
    <div class="flex flex-wrap gap-2">
      <button v-if="installPrompt" class="btn-ghost" @click="install">安装到桌面</button>
      <button v-if="waitingWorker" class="btn-ghost" @click="activateUpdate">更新并重新打开</button>
      <button class="btn-brand" :disabled="busy || !supported" @click="enable">{{ enabled ? '重新关联此设备通知' : '开启此设备通知' }}</button>
      <button v-if="enabled" class="btn-ghost" :disabled="busy" @click="disable">关闭此设备通知</button>
      <button v-if="enabled" class="btn-ghost" :disabled="busy" @click="sendTest">发送测试通知</button>
    </div>
    <p class="text-sm" role="status">{{ message }}</p>
    <p v-for="r in statuses.slice(0,3)" :key="r.id" class="text-xs text-muted-foreground">{{ r.last_status || '等待投递' }} · 尝试 {{ r.attempts }} 次</p>
  </section>
</template>
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useAuthStore } from '../stores/auth'
import { getPushConfig, saveSubscription, removeSubscription, testPush, getPushStatus } from '../api/automation'
import { registerPWA, pushRegistration, installPrompt, waitingWorker, activateUpdate, setPushAudience } from '../utils/pwa'
import { existingPushSubscription } from '../utils/push-subscription'
const auth=useAuthStore(), enabled=ref(false), busy=ref(false), message=ref(''), statuses=ref<{id:number;last_status:string;attempts:number}[]>([])
const supported=window.isSecureContext && 'serviceWorker' in navigator && 'PushManager' in window && 'Notification' in window
const ios=/iPhone|iPad|iPod/.test(navigator.userAgent) || (navigator.platform==='MacIntel' && navigator.maxTouchPoints>1)
const standalone=window.matchMedia('(display-mode: standalone)').matches || !!(navigator as Navigator & {standalone?:boolean}).standalone
const guidance=computed(()=>!window.isSecureContext ? '请使用 HTTPS 地址打开，才能安装和接收通知。' : ios&&!standalone ? 'Safari：打开分享菜单，选择“添加到主屏幕”，再从桌面图标打开并开启通知。' : !supported ? '当前浏览器未提供设备推送。站内提醒仍可使用。' : '开启后可在应用关闭时接收提醒。通知仅提示有新消息，账单详情需登录后查看。')
onMounted(async()=>{try{const r=await registerPWA();enabled.value=!!(await existingPushSubscription(r))}catch{message.value='无法读取设备通知状态，请稍后重试。'}finally{await loadStatus()}})
async function loadStatus(){try{statuses.value=(await getPushStatus()).data.data||[]}catch{}}
async function install(){await installPrompt.value?.prompt();installPrompt.value=null}
async function enable(){
 busy.value=true;message.value=''
 try{
  // Permission is requested directly from the user's button gesture (Safari).
  const permission=await Notification.requestPermission()
  if(permission!=='granted'){message.value='通知未授权，可在浏览器或系统设置中允许后重试。';return}
  const reg=await pushRegistration();const config=(await getPushConfig()).data.data
  const key=config.public_key.replace(/-/g,'+').replace(/_/g,'/');const bytes=Uint8Array.from(atob(key.padEnd(Math.ceil(key.length/4)*4,'=')),c=>c.charCodeAt(0))
  const sub=await existingPushSubscription(reg) || await reg.pushManager.subscribe({userVisibleOnly:true,applicationServerKey:bytes})
  await saveSubscription(sub.toJSON());await setPushAudience(auth.user.id);enabled.value=true;message.value='此设备通知已开启，可发送测试通知验证。'
 }catch(e){message.value=(e as {response?:{data?:{message?:string}}}).response?.data?.message || (e as Error).message || '开启失败，请检查系统通知设置后重试。'}finally{busy.value=false}
}
async function disable(){busy.value=true;try{await setPushAudience(null);const r=await pushRegistration();const s=await existingPushSubscription(r);if(s){await s.unsubscribe();await removeSubscription(s.endpoint)}enabled.value=false;message.value='此设备通知已关闭，站内提醒继续保留。'}catch{message.value='请检查网络并重试。'}finally{busy.value=false}}
async function sendTest(){busy.value=true;try{await testPush();message.value='测试已排队。请检查系统通知；服务接收状态不等于设备已送达。';await loadStatus()}catch{message.value='测试未发出，请稍后重试。'}finally{busy.value=false}}
</script>
