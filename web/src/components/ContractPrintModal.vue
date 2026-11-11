<template>
  <Modal v-model="show" title="合同 PDF 与版本归档" wide>
    <div class="space-y-4">
      <p class="text-sm text-muted-foreground">{{ tenant?.name }} · 资料修改与再次续租不会改变已归档的 PDF。生成后打印签字，再从合同附件入口上传签署件。</p>
      <div class="grid sm:grid-cols-2 gap-3">
        <label class="text-sm">平台模板<select v-model="templateId" class="input-field mt-1" @change="resetKey">
          <option v-for="t in templates" :key="t.id" :value="t.id">{{ t.title }} · v{{ t.version }}</option>
        </select></label>
        <label v-if="selected?.kind === 'renewal'" class="text-sm">本次续租<select v-model="renewalId" class="input-field mt-1" @change="resetKey">
          <option :value="0">请选择续租记录</option>
          <option v-for="r in renewals.filter((r) => r.effective_from)" :key="r.id" :value="r.id">{{ r.effective_from }} 至 {{ r.to_end_date }} · {{ r.rent_after }} 元/月</option>
        </select></label>
      </div>
      <button type="button" class="btn-brand" :disabled="loading || saving || !selected || (selected.kind === 'renewal' && !renewalId)" @click="generate">{{ saving ? '正在生成并归档…' : '生成并归档 PDF' }}</button>
      <p v-if="error" class="text-sm text-destructive">{{ error }}</p>
      <p class="text-sm font-semibold">已归档合同版本</p>
      <IssuedDocumentList :documents="documents" />
    </div>
  </Modal>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Modal from './Modal.vue'
import IssuedDocumentList from './IssuedDocumentList.vue'
import { getAgreements, getDocumentTemplates, issueAgreement, type DocumentTemplate, type IssuedDocument } from '../api/documents'
import { getTenantRenewals, type LeaseRenewal } from '../api/renew'
import type { RoomView, Tenant } from '../api/rental'
import { toast } from '../utils/toast'
const props = defineProps<{ modelValue: boolean; tenant: Tenant | null; room?: RoomView | null }>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()
const show = computed({ get: () => props.modelValue, set: (v: boolean) => emit('update:modelValue', v) })
const templates = ref<DocumentTemplate[]>([]), documents = ref<IssuedDocument[]>([]), renewals = ref<LeaseRenewal[]>([])
const templateId = ref(0), renewalId = ref(0), error = ref(''), saving = ref(false), loading = ref(false)
const selected = computed(() => templates.value.find((t) => t.id === templateId.value))
let key = ''
function resetKey() { key = crypto.randomUUID() }
watch(() => props.modelValue, async (open) => {
  if (!open || !props.tenant) return
  loading.value = true; error.value = ''; renewalId.value = 0; documents.value = []; resetKey()
  try {
    const [t, d, r] = await Promise.all([getDocumentTemplates(), getAgreements(props.tenant.id), getTenantRenewals(props.tenant.id)])
    templates.value = t.data.data || []; templateId.value = templates.value.find((v) => v.kind === 'lease')?.id || 0
    documents.value = d.data.data || []; renewals.value = r.data.data?.items || []
  } catch { error.value = '合同资料加载失败，请关闭后重试' }
  finally { loading.value = false }
})
async function generate() {
  if (!props.tenant || !selected.value) return
  saving.value = true; error.value = ''
  try {
    const res = await issueAgreement(props.tenant.id, { kind: selected.value.kind, template_id: templateId.value, renewal_id: selected.value.kind === 'renewal' ? renewalId.value : 0, request_key: key })
    if (res.data?.code !== 0) { error.value = res.data?.message || '生成失败'; return }
    documents.value = (await getAgreements(props.tenant.id)).data.data || []
    resetKey(); toast('合同 PDF 已生成并归档')
  } catch (e) { error.value = (e as { response?: { data?: { message?: string } } }).response?.data?.message || '生成失败，请使用当前表单重试' }
  finally { saving.value = false }
}
</script>
