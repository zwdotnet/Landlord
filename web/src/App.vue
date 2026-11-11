<template>
  <router-view />
  <Toast :message="toastText(toastState.message)" :type="toastState.type" :key="toastState.message" />
</template>

<script setup lang="ts">
import { watch } from 'vue'
import { useAuthStore } from './stores/auth'
import { registerPWA, setPushAudience } from './utils/pwa'
import { saveSubscription } from './api/automation'
import Toast from './components/Toast.vue'
import { toastState, toastText } from './utils/toast'
const auth = useAuthStore()
let generation = 0
watch(() => auth.user?.id, async (id) => {
 const current = ++generation
 const reg = await registerPWA()
 if(current !== generation) return
 await setPushAudience(id || null)
 if(id && reg) {
  const sub = await reg.pushManager?.getSubscription()
  if(sub && current === generation) await saveSubscription(sub.toJSON()).catch(() => null)
 }
}, { immediate: true })
</script>
