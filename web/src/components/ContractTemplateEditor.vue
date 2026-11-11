<template>
  <section class="surface rounded-2xl p-5 space-y-3 mt-4">
    <h2 class="font-semibold">平台合同模板</h2>
    <p class="text-xs text-muted-foreground">修改正文将创建新版本，已归档的合同和 PDF 使用原版本。</p>
    <select v-model="kind" class="input-field" @change="fill"><option value="lease">住宅租赁合同</option><option value="renewal">续租协议</option></select>
    <p class="text-xs text-muted-foreground">当前版本：{{ selected?.version || '—' }}</p>
    <label class="block text-sm">标题<input v-model="title" maxlength="80" class="input-field mt-1" /></label>
    <label class="block text-sm">正文条款（条款之间空一行）<textarea v-model="clauses" class="input-field mt-1 min-h-64" /></label>
    <p v-if="message" class="text-sm">{{ message }}</p>
    <button type="button" class="btn-brand" :disabled="saving" @click="save">{{ saving ? '保存中…' : '保存为新版本' }}</button>
  </section>
</template>
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { getDocumentTemplates, publishDocumentTemplate, type DocumentTemplate } from '../api/documents'
const rows = ref<DocumentTemplate[]>([]), kind = ref<'lease' | 'renewal'>('lease')
const title = ref(''), clauses = ref(''), message = ref(''), saving = ref(false)
const selected = computed(() => rows.value.find((r) => r.kind === kind.value))
function fill() { title.value = selected.value?.title || ''; clauses.value = selected.value?.clauses.join('\n\n') || '' }
async function load() { const res = await getDocumentTemplates(true); rows.value = res.data.data || []; fill() }
onMounted(() => load().catch(() => { message.value = '模板加载失败' }))
async function save() {
  saving.value = true; message.value = ''
  try {
    const res = await publishDocumentTemplate({ kind: kind.value, title: title.value, clauses: clauses.value.split(/\n\s*\n/).map((s) => s.trim()).filter(Boolean) })
    if (res.data?.code !== 0) { message.value = res.data?.message || '保存失败'; return }
    await load(); message.value = '新版本已保存'
  } catch (e) { message.value = (e as { response?: { data?: { message?: string } } }).response?.data?.message || '保存失败' }
  finally { saving.value = false }
}
</script>
