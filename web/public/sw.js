const CACHE = 'phaze-shell-v2'

self.addEventListener('install', (event) => {
  event.waitUntil(self.skipWaiting())
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim())
  )
})

self.addEventListener('fetch', (event) => {
  const req = event.request
  if (req.method !== 'GET') return
  const url = new URL(req.url)
  if (url.origin !== self.location.origin) return

  // Never cache: API, websocket, uploads, or anything dynamic.
  if (url.pathname.startsWith('/api') || url.pathname.startsWith('/ws') ||
      url.pathname.startsWith('/uploads') || url.pathname.startsWith('/public')) return

  // HTML navigation: always network-first so deploys take effect immediately.
  // Fall back to cache only when offline.
  if (req.mode === 'navigate' || url.pathname.endsWith('.html') ||
      url.pathname === '/web/' || url.pathname === '/web') {
    event.respondWith(
      fetch(req).then((resp) => {
        if (resp.ok) {
          const copy = resp.clone()
          caches.open(CACHE).then((c) => c.put(req, copy))
        }
        return resp
      }).catch(() => caches.match(req).then((hit) => hit || caches.match('/web/index.html')))
    )
    return
  }

  // Hashed assets (JS/CSS with content hash in filename): cache-first, they never change.
  if (url.pathname.startsWith('/web/assets/')) {
    event.respondWith(
      caches.match(req).then((hit) => {
        if (hit) return hit
        return fetch(req).then((resp) => {
          if (resp.ok) {
            const copy = resp.clone()
            caches.open(CACHE).then((c) => c.put(req, copy))
          }
          return resp
        })
      })
    )
    return
  }

  // Everything else: network with cache fallback.
  event.respondWith(
    fetch(req).then((resp) => {
      if (resp.ok && resp.type === 'basic') {
        const copy = resp.clone()
        caches.open(CACHE).then((c) => c.put(req, copy))
      }
      return resp
    }).catch(() => caches.match(req).then((hit) => hit || Response.error()))
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
