// Bump CACHE on each deploy so stale JS/CSS never sticks around. The hashed
// asset filenames Vite emits make precaching safe; we cache the shell on demand
// (runtime) rather than listing every hashed file here.
const CACHE = 'phaze-shell-v1'
const SHELL = ['/web/', '/web/index.html', '/web/manifest.json', '/web/favicon.svg', '/web/icon-192.png', '/web/icon-512.png']

self.addEventListener('install', (event) => {
  event.waitUntil(caches.open(CACHE).then((c) => c.addAll(SHELL)).then(() => self.skipWaiting()))
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim())
  )
})

// Only the static app shell is cached. API and websocket traffic always hits the
// network and is never stored (DM bodies are E2EE blobs; responses are dynamic).
self.addEventListener('fetch', (event) => {
  const req = event.request
  if (req.method !== 'GET') return
  const url = new URL(req.url)
  if (url.origin !== self.location.origin) return
  if (url.pathname.startsWith('/api') || url.pathname.startsWith('/ws') ||
      url.pathname.startsWith('/uploads') || url.pathname.startsWith('/public')) return

  // Cache-first for the static shell; fall back to network and refresh the cache.
  event.respondWith(
    caches.match(req).then((hit) => {
      if (hit) return hit
      return fetch(req).then((resp) => {
        if (resp.ok && resp.type === 'basic') {
          const copy = resp.clone()
          caches.open(CACHE).then((c) => c.put(req, copy))
        }
        return resp
      }).catch(() => {
        // Offline navigation falls back to the cached app shell.
        if (req.mode === 'navigate') return caches.match('/web/index.html')
        return Response.error()
      })
    })
  )
})

self.addEventListener('push', (event) => {
  const data = event.data ? event.data.json() : {}
  const title = data.title || 'Phaze'
  const body = data.body || 'New message'
  event.waitUntil(
    self.registration.showNotification(title, {
      body,
      icon: '/web/favicon.svg',
      badge: '/web/favicon.svg',
      tag: 'phaze-msg',
      renotify: true,
    })
  )
})

self.addEventListener('notificationclick', (event) => {
  event.notification.close()
  event.waitUntil(clients.openWindow('/web/'))
})
