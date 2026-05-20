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
