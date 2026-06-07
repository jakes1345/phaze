import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import * as Sentry from '@sentry/react'
import './index.css'
import App from './App.tsx'

// VITE_SENTRY_DSN comes from build env. When unset, Sentry.init is a no-op
// — the SDK ships in the bundle but emits nothing. Defense-in-depth: we
// scrub WS payloads in beforeSend because DM bodies are E2EE blobs anyway
// and shouldn't ride out to a third party even by accident.
const dsn = (import.meta.env.VITE_SENTRY_DSN as string | undefined) || ''
if (dsn) {
  Sentry.init({
    dsn,
    environment: (import.meta.env.VITE_SENTRY_ENV as string) || 'production',
    release: (import.meta.env.VITE_BUILD_VERSION as string) || undefined,
    tracesSampleRate: 0.05,
    beforeSend(event) {
      if (event.request?.data) event.request.data = '[scrubbed]'
      return event
    },
  })
}

// Register the service worker for the offline app shell. This is independent of
// push: push subscription is requested later, on user opt-in, in Settings. We
// skip it in dev so Vite's HMR isn't shadowed by a cached shell.
if ('serviceWorker' in navigator && import.meta.env.PROD) {
  window.addEventListener('load', () => {
    navigator.serviceWorker
      .register('/web/sw.js', { scope: '/web/' })
      .catch((e) => console.warn('[sw] registration failed', e))
  })
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <Sentry.ErrorBoundary fallback={<div style={{ padding: 24, fontFamily: 'Inter, system-ui, sans-serif' }}>Something broke. Refresh to retry.</div>}>
      <App />
    </Sentry.ErrorBoundary>
  </StrictMode>,
)
