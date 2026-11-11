<template>
  <div v-if="!bill.recipient_tenant_ids?.length" class="rounded-xl border border-border p-3 space-y-2">
    <p class="text-sm font-medium text-foreground">核对历史账单归属</p>
    <p class="text-xs text-muted-foreground">这张账单尚未关联租客，暂时仅房东可见。请核对原始合同和入住记录后选择所属租客；确认后不能在此重新分配。</p>
    <p v-if="loading" class="text-xs text-muted-foreground">加载租客档案中…</p>
    <label v-for="tenant in tenants" :key="tenant.id" class="flex items-start gap-2 text-sm">
      <input v-model="selected" type="checkbox" :value="tenant.id" class="mt-1" />
      <span>{{ tenant.name }} · {{ tenant.phone || '未留电话' }}<br />
        <span class="text-xs text-muted-foreground">{{ tenant.move_in_date || '入住日期未填' }} 至 {{ tenant.move_out_date || '仍在租' }}</span>
      </span>
    </label>
    <p v-if="!loading && !tenants.length && !error" class="text-xs text-muted-foreground">没有可选租客，请先补齐租客档案。</p>
    <p v-if="error" class="text-xs text-destructive">{{ error }}</p>
    <button type="button" class="btn-ghost" :disabled="loading || saving || !selected.length" @click="save">{{ saving ? '保存中…' : '确认归属' }}</button>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { assignBillRecipients, getAllTenants, type Bill, type Tenant } from '../api/rental'

const props = defineProps<{ bill: Bill }>()
const emit = defineEmits<{ assigned: [bill: Bill] }>()
const tenants = ref<Tenant[]>([])
const selected = ref<number[]>([])
const loading = ref(false)
const saving = ref(false)
const error = ref('')
watch(() => props.bill.id, async () => {
  selected.value = []
  tenants.value = []
  error.value = ''
  if (props.bill.recipient_tenant_ids?.length) return
  loading.value = true
  try {
    tenants.value = await getAllTenants({ room_id: props.bill.room_id })
  } catch {
    error.value = '租客档案加载失败，请确认有租客查看权限后重试'
  } finally {
    loading.value = false
  }
}, { immediate: true })

async function save() {
  saving.value = true
  error.value = ''
  try {
    const res = await assignBillRecipients(props.bill.id, selected.value)
    if (res.data?.code !== 0) {
      error.value = res.data?.message || '保存失败，请核对归属后重试'
      return
    }
    emit('assigned', res.data.data)
  } catch {
    error.value = '保存失败，请核对租客的入住、退租日期后重试'
  } finally {
    saving.value = false
  }
}
</script>
