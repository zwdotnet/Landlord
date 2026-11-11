<template><RouterLink :to="auth.user?.role==='tenant' ? '/portal/notifications' : '/admin/notifications'" class="text-xs sm:text-sm text-brand-600 whitespace-nowrap p-2 rounded-lg hover:bg-muted" aria-label="消息与推送">提醒<span v-if="unread" class="ml-1 text-destructive">{{ unread>99?'99+':unread }}</span></RouterLink></template>
<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { watch } from 'vue'
import { getInbox } from '../api/automation'
import { useAuthStore } from '../stores/auth'
const auth=useAuthStore(),unread=ref(0),route=useRoute();let timer:ReturnType<typeof setInterval>|undefined
async function load(){try{unread.value=(await getInbox()).data.data?.unread||0}catch{}}
onMounted(()=>{load();timer=setInterval(load,60000)});onBeforeUnmount(()=>clearInterval(timer));watch(()=>route.path,load)
</script>
