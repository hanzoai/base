import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// Admin UI base path. Default '/_/'. Set BASE_ADMIN_UI_PATH (e.g. '/admin/') to
// relocate the dashboard — the Go server reads the SAME env to mount it, so the
// SPA's absolute asset URLs line up. Normalized to a '/x/' form. One knob, set
// at build+deploy together (same contract as BASE_API_PREFIX ↔ VITE_API_PREFIX).
const adminBase = (() => {
  const p = (process.env.BASE_ADMIN_UI_PATH || '').replace(/^\/+|\/+$/g, '')
  return p ? `/${p}/` : '/_/'
})()

export default defineConfig({
  plugins: [react()],
  base: adminBase,
  resolve: {
    alias: { '~': '/src' },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    rollupOptions: {
      output: { manualChunks: undefined },
    },
  },
  server: {
    port: 3000,
    proxy: {
      '/api': 'http://localhost:8090',
      '/realtime': {
        target: 'http://localhost:8090',
        changeOrigin: true,
        ws: true,
      },
    },
  },
})
