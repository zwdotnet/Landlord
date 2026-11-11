<template>
 <div class="page-container space-y-5">
  <PageHeader title="周期任务" description="每月抄表、自动出账和提醒；按房间的明确入住关系执行"><template #actions><button class="btn-ghost" :disabled="busy" @click="run">立即检查到期任务</button><RouterLink to="/admin/meters" class="btn-ghost">录入抄表</RouterLink></template></PageHeader>
  <p class="text-sm text-muted-foreground">新计划默认沿用租客缴费日，抄表、出账提前 3 天，提醒再提前 3 天。按香港时间每日 08:00 起处理，后台每分钟检查并在重启后补偿。缺读数时等待补录，不会按零用量出账。</p>
  <form v-if="ledger.canEdit('bills') && ledger.canEdit('meters')" class="surface rounded-2xl p-4 sm:p-6 space-y-4" @submit.prevent="save">
   <h2 class="font-semibold">{{ form.id ? '确认或修改计划' : '建立周期计划' }}</h2>
   <div class="grid sm:grid-cols-2 lg:grid-cols-3 gap-4">
    <label class="text-sm">房源<select v-model.number="form.room_id" required class="input-field mt-1" @change="chooseRoom"><option :value="0">请选择房源</option><option v-for="r in rooms" :key="r.id" :value="r.id">{{ r.room_no }}</option></select></label>
    <label class="text-sm">开始账期<input v-model="form.start_period" required :disabled="!!form.id" type="month" class="input-field mt-1" /></label>
    <label class="text-sm">抄表日（每月）<input v-model.number="form.meter_day" type="number" min="1" max="28" required class="input-field mt-1" /></label>
    <label class="text-sm">出账日（每月）<input v-model.number="form.bill_day" type="number" min="1" max="28" required class="input-field mt-1" /></label>
    <label class="text-sm">缴费日（每月）<input v-model.number="form.due_day" type="number" min="1" max="28" required class="input-field mt-1" /></label>
    <label class="text-sm">提前提醒天数<input v-model.number="form.lead_days" type="number" min="0" max="30" required class="input-field mt-1" /></label>
   </div>
   <fieldset><legend class="text-sm mb-2">本计划的承租关系（共同承租合开一张房屋账单）</legend><div class="flex flex-wrap gap-3"><label v-for="t in roomTenants" :key="t.id" class="text-sm"><input v-model="form.tenant_ids" type="checkbox" :value="t.id" /> {{ t.name }}</label></div></fieldset>
   <label class="flex gap-2 items-center text-sm"><input v-model="form.enabled" type="checkbox" /> 启用本计划的自动出账和提醒</label>
   <p class="text-xs text-muted-foreground">保存即确认所选租客的当前租期、租金与费用约定。后续续租或费用变更会暂停未出账任务，需在此重新保存确认。旧账单和旧合同 PDF 保留。</p>
   <button class="btn-brand" :disabled="busy || !form.tenant_ids.length">{{ busy ? '保存中…' : '保存并确认条款' }}</button>
  </form>
  <p v-if="message" role="status" class="text-sm">{{ message }}</p>
  <section class="surface rounded-2xl p-4 sm:p-6 space-y-3"><h2 class="font-semibold">已保存计划</h2><p v-if="!plans.length" class="text-sm text-muted-foreground">尚未建立计划，现有租客不会自动启用。</p><div v-for="p in plans" :key="p.id" class="flex flex-wrap justify-between items-center border-t border-border pt-3 gap-2"><div class="text-sm"><strong>{{ p.room_no }}</strong> · {{ p.enabled ? '已启用' : '已暂停' }} · 抄表 {{ p.meter_day }} 日 / 出账 {{ p.bill_day }} 日 / 缴费 {{ p.due_day }} 日 <span class="text-xs text-muted-foreground">v{{ p.version }}</span></div><button class="btn-ghost text-sm" @click="edit(p)">修改 / 确认</button></div></section>
  <section class="space-y-3"><h2 class="font-semibold">任务记录</h2><p v-if="!tasks.length" class="text-sm text-muted-foreground">任务会在提醒日期到达时建立，也可点击立即检查。</p><article v-for="t in tasks" :key="t.id" class="surface rounded-2xl p-4 space-y-2"><div class="flex justify-between gap-2 text-sm"><strong>{{ t.room_no }} · {{ t.period }}</strong><span :class="t.status==='issued' ? 'text-emerald-600' : 'text-amber-700'">{{ statusText(t.status) }}</span></div><p class="text-sm break-words">{{ t.detail }}</p><p class="text-xs text-muted-foreground">抄表 {{ t.meter_date }} · 出账 {{ t.bill_date }} · 缴费 {{ t.due_date }}</p><RouterLink v-if="t.bill_id" to="/admin/bills" class="text-sm text-brand-600">查看账单</RouterLink></article></section>
 </div>
</template>
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import PageHeader from '../components/PageHeader.vue'
import { getAllRooms, getAllTenants, type RoomView, type Tenant } from '../api/rental'
import { useLedgerStore } from '../stores/ledger'
import { getAutomationPlans, getCycleTasks, saveAutomationPlan, checkAutomation, type AutomationPlan, type CycleTask } from '../api/automation'
const ledger=useLedgerStore(), rooms=ref<RoomView[]>([]), tenants=ref<Tenant[]>([]), plans=ref<AutomationPlan[]>([]),tasks=ref<CycleTask[]>([]), busy=ref(false),message=ref('')
const today=new Intl.DateTimeFormat('en-CA',{timeZone:'Asia/Hong_Kong',year:'numeric',month:'2-digit'}).format(new Date()).replace('/','-')
function blank():Partial<AutomationPlan>&{room_id:number;tenant_ids:number[]}{return {room_id:0,tenant_ids:[],enabled:true,meter_day:7,bill_day:7,due_day:10,lead_days:3,start_period:today}}
const form=ref(blank()),roomTenants=computed(()=>tenants.value.filter(t=>t.room_id===form.value.room_id && (t.active || form.value.tenant_ids.includes(t.id))))
function chooseRoom(){const r=rooms.value.find(r=>r.id===form.value.room_id);form.value.id=undefined;form.value.tenant_ids=tenants.value.filter(t=>t.room_id===form.value.room_id&&t.active).map(t=>t.id);const due=r?.billing.pay_day||10;form.value.due_day=due;form.value.bill_day=Math.max(1,due-3);form.value.meter_day=form.value.bill_day}
function edit(p:AutomationPlan){form.value={...p,tenant_ids:[...p.tenant_ids]};window.scrollTo({top:0,behavior:'smooth'})}
async function load(){try{const [p,t,r,ts]=await Promise.all([getAutomationPlans(),getCycleTasks(),getAllRooms(),getAllTenants()]);plans.value=p.data.data||[];tasks.value=t.data.data||[];rooms.value=r;tenants.value=ts}catch{message.value='加载失败，请检查当前台账权限。'}}
async function save(){busy.value=true;try{await saveAutomationPlan(form.value);await checkAutomation();await load();message.value='计划已保存，已检查到期任务。'}catch(e){message.value=(e as {response?:{data?:{message?:string}}}).response?.data?.message||'保存失败'}finally{busy.value=false}}
async function run(){busy.value=true;try{await checkAutomation();await load();message.value='检查完成，缺读数的任务继续等待补录。'}catch{message.value='检查失败，请稍后重试。'}finally{busy.value=false}}
function statusText(s:string){return ({issued:'已出账',waiting_meter:'等待抄表',waiting_previous:'等待前期',review:'需要确认',pending:'等待出账日',retry:'等待重试',cancelled:'租期外已取消'} as Record<string,string>)[s]||s}
onMounted(load);watch(()=>ledger.current?.id,()=>{form.value=blank();load()})
</script>
