import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'

// `server.proxy` is dev-only; production builds ignore it. Vitest merges this file as a plain object.
const devEnv = loadEnv('development', process.cwd(), '')
const nexusOrigin = devEnv.VITE_DEV_NEXUS_ORIGIN || 'http://127.0.0.1:8080'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  // The production server mounts the SPA at /web/, so every absolute asset
  // URL must be prefixed with /web/. Override via VITE_BASE for separate-host
  // deploys (GitHub Pages, Vercel) where the SPA is served at the root.
  base: process.env.VITE_BASE || '/web/',
  server: {
    proxy: {
      '/public': {
        target: nexusOrigin,
        changeOrigin: true,
      },
    },
  },
})
