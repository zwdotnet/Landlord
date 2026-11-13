export async function existingPushSubscription(reg: ServiceWorkerRegistration | null): Promise<PushSubscription | null> {
  // No usable subscription exists before permission is granted. Querying the
  // native push service in that state can freeze Windows Playwright WebKit.
  if (!reg || typeof Notification === 'undefined' || Notification.permission !== 'granted') return null
  return await reg.pushManager?.getSubscription() ?? null
}
