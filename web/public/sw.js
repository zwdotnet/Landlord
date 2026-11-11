/* __CACHE_VERSION__ is replaced by Vite with a fingerprint of this build. */
const SCOPE = self.registration.scope;
const PREFIX = 'landlord-shell:' + SCOPE + ':';
const SHELL = PREFIX + '__CACHE_VERSION__';
const META = 'landlord-push-meta-' + SCOPE;
const stateURL = new URL('__push_account', SCOPE).href;
self.addEventListener('install', event => {
  event.waitUntil(caches.open(SHELL).then(cache => cache.addAll(['offline.html', 'favicon-192.png', 'favicon-512.png'].map(p => new URL(p, SCOPE).href))));
});
self.addEventListener('activate', event => {
  event.waitUntil(caches.keys().then(keys => Promise.all(keys.filter(k => k.startsWith(PREFIX) && k !== SHELL).map(k => caches.delete(k)))).then(() => self.clients.claim()));
});
self.addEventListener('message', event => {
  if (event.data?.type === 'SKIP_WAITING') self.skipWaiting();
  if (event.data?.type === 'SET_PUSH_USER') event.waitUntil(caches.open(META).then(c => c.put(stateURL, new Response(String(event.data.userId || '')))));
});
self.addEventListener('fetch', event => {
  const u = new URL(event.request.url);
  if (event.request.method !== 'GET' || u.origin !== self.location.origin || !u.href.startsWith(SCOPE)) return;
  // Never cache authenticated APIs, PDFs, uploads, SPA responses or credentials.
  if (event.request.mode === 'navigate') {
    event.respondWith(fetch(event.request).catch(() => caches.match(new URL('offline.html', SCOPE).href)));
  } else if (['offline.html', 'favicon-192.png', 'favicon-512.png'].some(p => u.href === new URL(p, SCOPE).href)) {
    event.respondWith(caches.match(event.request).then(cached => cached || fetch(event.request)));
  }
});
self.addEventListener('push', event => {
  event.waitUntil((async () => {
    let data;
    try { data = event.data.json(); } catch { return; }
    const state = await (await caches.open(META)).match(stateURL);
    if (!state || String(data.user_id) !== await state.text()) return;
    await self.registration.showNotification(data.title || '租务提醒', {
      body: data.body || '打开平台查看新消息。', tag: data.tag || 'landlord-reminder',
      icon: new URL('favicon-192.png', SCOPE).href, badge: new URL('favicon-192.png', SCOPE).href,
      data: { path: data.url || '/notifications' },
    });
  })());
});
self.addEventListener('notificationclick', event => {
  event.notification.close();
  event.waitUntil((async () => {
    const path = String(event.notification.data?.path || '').replace(/^\/+/, '');
    const target = new URL(path, SCOPE);
    if (target.origin !== self.location.origin || !target.href.startsWith(SCOPE)) return;
    const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
    const existing = windows.find(w => w.url.startsWith(SCOPE));
    if (existing) { await existing.navigate(target.href); return existing.focus(); }
    return self.clients.openWindow(target.href);
  })());
});
