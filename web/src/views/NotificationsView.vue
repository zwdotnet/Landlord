<template>
 <div class="page-container space-y-5">
  <PageHeader title="消息与推送" :description="`未读 ${unread} 条 · 消息按当前账号权限展示`"><template #actions><button class="btn-ghost" @click="load">刷新</button></template></PageHeader>
  <PushSettings />
  <p v-if="error" role="alert" class="text-destructive text-sm">{{ error }}</p>
  <p v-if="!rows.length" class="surface rounded-2xl p-6 text-muted-foreground">暂无消息。周期计划开启后，抄表待办和新账单会在这里显示。</p>
  <article v-for="n in rows" :key="n.id" class="surface rounded-2xl p-4 sm:p-5 space-y-2" :class="!n.read_at ? 'border border-brand-300' : ''">
   <div class="flex justify-between gap-3"><h2 class="font-semibold">{{ n.title }} <span v-if="!n.read_at" class="text-xs text-brand-600">未读</span></h2><time class="text-xs text-muted-foreground shrink-0">{{ n.created_at.slice(0,16).replace('T',' ') }}</time></div>
   <p class="text-sm text-muted-foreground break-words">{{ n.body }}</p>
   <div class="flex gap-4 text-sm"><button class="text-brand-600" @click="open(n)">查看相关信息</button><button v-if="!n.read_at" class="text-muted-foreground" @click="mark(n)">标为已读</button></div>
  </article>
 </div>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import PageHeader from '../components/PageHeader.vue'
import PushSettings from '../components/PushSettings.vue'
import { getInbox, readNotice, type InboxNotice } from '../api/automation'
const router=useRouter(), rows=ref<InboxNotice[]>([]), unread=ref(0), error=ref('')
async function load(){try{const r=(await getInbox()).data.data;rows.value=r.items||[];unread.value=r.unread||0;error.value=''}catch{error.value='消息加载失败，请稍后重试。'}}
async function mark(n:InboxNotice){try{await readNotice(n.id);await load()}catch{error.value='保存失败，请稍后重试。'}}
async function open(n:InboxNotice){if(!n.read_at)await mark(n);if(n.url.startsWith('/admin/')||n.url.startsWith('/portal/'))await router.push(n.url)}
onMounted(load)
</script>
