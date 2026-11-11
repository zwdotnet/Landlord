import { ref } from 'vue'
export const waitingWorker = ref<ServiceWorker | null>(null)
export const installPrompt = ref<(Event & { prompt: () => Promise<void>; userChoice: Promise<{ outcome: string }> }) | null>(null)
let registration: Promise<ServiceWorkerRegistration | null> | null = null
export function registerPWA() {
  if (registration) return registration
  if (!('serviceWorker' in navigator) || !window.isSecureContext) return Promise.resolve(null)
  registration = navigator.serviceWorker.register(`${import.meta.env.BASE_URL}sw.js`, { scope: import.meta.env.BASE_URL }).then((reg) => {
    if (reg.waiting) waitingWorker.value = reg.waiting
    reg.addEventListener('updatefound', () => {
      const worker = reg.installing
      worker?.addEventListener('statechange', () => { if (worker.state === 'installed' && navigator.serviceWorker.controller) waitingWorker.value = worker })
    })
    return reg
  }).catch(() => null)
  window.addEventListener('beforeinstallprompt', (event) => { event.preventDefault(); installPrompt.value = event as typeof installPrompt.value })
  return registration
}
export async function pushRegistration() {
  const reg = await registerPWA()
  if (!reg) throw new Error('请通过 HTTPS 或本机地址打开平台')
  return await navigator.serviceWorker.ready
}
export async function setPushAudience(id: number | null) {
  const reg = await registerPWA()
  reg?.active?.postMessage({ type: 'SET_PUSH_USER', userId: id })
}
export async function detachPush(token: string) {
  // Clear the worker's account gate immediately; unregister only this device's
  // push subscription, preserving the app's service worker and other sites.
  await setPushAudience(null)
  const reg = await registerPWA()
  const sub = await reg?.pushManager?.getSubscription()
  if (!sub) return
  const endpoint = sub.endpoint
  await sub.unsubscribe().catch(() => false)
  if (token) await fetch(`${import.meta.env.BASE_URL}api/rental/push/subscriptions`, { method: 'DELETE', credentials: 'same-origin', headers: { 'Content-Type':'application/json', Authorization:`Bearer ${token}` }, body: JSON.stringify({ endpoint }) }).catch(() => null)
}
export function activateUpdate() {
  if (!waitingWorker.value) return
  navigator.serviceWorker.addEventListener('controllerchange', () => location.reload(), { once: true })
  waitingWorker.value.postMessage({ type: 'SKIP_WAITING' })
}
