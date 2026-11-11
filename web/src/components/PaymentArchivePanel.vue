<template>
  <div class="space-y-2 mt-4">
    <p class="text-sm font-semibold text-foreground">逐笔收款与归档收据</p>
    <div v-for="p in payments" :key="p.id" class="border border-border rounded-xl p-3 space-y-1">
      <div class="flex flex-wrap justify-between gap-2 text-sm">
        <span>{{ p.paid_at.slice(0, 16).replace('T', ' ') }}</span>
        <strong :class="p.amount < 0 ? 'text-destructive' : 'text-emerald-600'">{{ p.amount < 0 ? '冲销 ' : '实收 ' }}{{ Math.abs(p.amount).toFixed(2) }} 元</strong>
      </div>
      <p v-if="p.bill_id && billPeriods" class="text-xs text-muted-foreground">账期 {{ billPeriods[p.bill_id] || '—' }}</p>
      <p v-if="p.items?.length" class="text-xs text-muted-foreground">{{ p.items.map((i) => `${i.name} ${i.amount.toFixed(2)} 元`).join(' · ') }}</p>
      <p v-if="p.note" class="text-xs text-muted-foreground">{{ p.note }}</p>
      <p v-if="p.voided_at" class="text-xs text-destructive">已冲销 · {{ p.void_reason }}</p>
      <div class="flex gap-3 text-xs py-1">
        <button v-if="p.receipt_id" type="button" class="text-brand-600" @click="view(p)">查看原始{{ p.amount < 0 ? '冲销凭证' : '收据' }}</button>
        <span v-else class="text-muted-foreground">历史流水尚无归档 PDF</span>
        <button v-if="canReverse && p.amount > 0 && !p.voided_at" type="button" class="text-destructive" @click="select(p)">冲销登记</button>
      </div>
    </div>
    <p v-if="!payments.length" class="text-sm text-muted-foreground">暂无收款。</p>
    <p v-if="error" class="text-sm text-destructive">{{ error }}</p>
    <Modal v-model="showPDF" title="原始收据" wide>
      <iframe v-if="url" :src="url" title="收据 PDF" class="w-full h-[65dvh] border-0" />
      <a :href="url" download="receipt.pdf" class="btn-brand inline-flex mt-3">下载 PDF</a>
    </Modal>
    <Modal v-model="showReverse" title="冲销收款登记">
      <form class="space-y-3" @submit.prevent="reverse">
        <p class="text-sm">更正本笔 {{ selected?.amount.toFixed(2) }} 元收款，原流水与原收据保留。此操作记录登记更正，不代表已向租客退款。</p>
        <label class="block text-sm">冲销原因<textarea v-model="reason" required maxlength="500" class="input-field mt-1 min-h-24" /></label>
        <p v-if="reverseError" class="text-sm text-destructive">{{ reverseError }}</p>
        <button type="submit" :disabled="saving || !reason.trim()" class="btn-brand">{{ saving ? '保存中…' : '确认整笔冲销' }}</button>
      </form>
    </Modal>
  </div>
</template>
<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import Modal from './Modal.vue'
import { fetchIssuedPDF, reversePayment, type ArchivedPayment } from '../api/documents'
const props = defineProps<{ payments: ArchivedPayment[]; portal?: boolean; canReverse?: boolean; billPeriods?: Record<number, string> }>()
const emit = defineEmits<{ changed: [] }>()
const error = ref(''), reverseError = ref(''), reason = ref(''), url = ref('')
const showPDF = ref(false), showReverse = ref(false), saving = ref(false)
const selected = ref<ArchivedPayment | null>(null)
let key = ''
function release() { if (url.value) URL.revokeObjectURL(url.value); url.value = '' }
onBeforeUnmount(release)
watch(showPDF, (v) => { if (!v) release() })
async function view(p: ArchivedPayment) {
  error.value = ''
  try { const blob = await fetchIssuedPDF(p.receipt_id!, props.portal); release(); url.value = URL.createObjectURL(blob); showPDF.value = true }
  catch { error.value = '原始凭证加载失败，请稍后重试' }
}
function select(p: ArchivedPayment) { selected.value = p; reason.value = ''; reverseError.value = ''; key = crypto.randomUUID(); showReverse.value = true }
async function reverse() {
  if (!selected.value) return
  saving.value = true
  reverseError.value = ''
  try {
    const res = await reversePayment(selected.value.id, reason.value, key)
    if (res.data?.code !== 0) { reverseError.value = res.data?.message || '冲销未保存'; return }
    showReverse.value = false
    emit('changed')
  } catch (e) { reverseError.value = (e as { response?: { data?: { message?: string } } }).response?.data?.message || '冲销未保存，请核对后重试' }
  finally { saving.value = false }
}
</script>
