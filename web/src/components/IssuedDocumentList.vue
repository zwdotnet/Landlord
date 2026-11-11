<template>
  <div class="space-y-2">
    <p v-if="!documents.length" class="text-sm text-muted-foreground py-3">暂无归档 PDF。</p>
    <div v-for="doc in documents" :key="doc.id" class="border border-border rounded-xl p-3 flex flex-wrap items-center justify-between gap-2">
      <div class="min-w-0 text-sm">
        <p class="font-medium text-foreground">{{ doc.kind === 'lease' ? '租赁合同' : '续租协议' }} · {{ doc.number }}</p>
        <p class="text-xs text-muted-foreground">模板 v{{ doc.template_version }} · {{ doc.created_at.slice(0, 16).replace('T', ' ') }} · {{ Math.ceil(doc.file_size / 1024) }} KB</p>
      </div>
      <button type="button" class="btn-ghost text-sm" :disabled="loading" @click="open(doc)">查看 / 打印 PDF</button>
    </div>
    <p v-if="error" class="text-sm text-destructive">{{ error }}</p>
    <Modal v-model="show" title="归档原件" wide>
      <iframe v-if="url" :src="url" title="归档 PDF" class="w-full h-[65dvh] border-0 rounded-lg" />
      <a v-if="url" :href="url" :download="filename" class="btn-brand inline-flex mt-3">下载 PDF</a>
    </Modal>
  </div>
</template>
<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import Modal from './Modal.vue'
import { fetchIssuedPDF, type IssuedDocument } from '../api/documents'
const props = defineProps<{ documents: IssuedDocument[]; portal?: boolean }>()
const show = ref(false)
const loading = ref(false)
const error = ref('')
const url = ref('')
const filename = ref('')
function release() { if (url.value) URL.revokeObjectURL(url.value); url.value = '' }
watch(show, (value) => { if (!value) release() })
onBeforeUnmount(release)
async function open(doc: IssuedDocument) {
  loading.value = true
  error.value = ''
  try {
    const blob = await fetchIssuedPDF(doc.id, props.portal)
    release()
    url.value = URL.createObjectURL(blob)
    filename.value = doc.number + '.pdf'
    show.value = true
  } catch { error.value = '归档文件读取失败，请稍后重试' }
  finally { loading.value = false }
}
</script>
