import { TanStackRouterVite } from '@tanstack/router-plugin/vite'
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
  // Router plugin generates src/routeTree.gen.ts from src/routes/** (gitignored).
  plugins: [TanStackRouterVite({ target: 'react', autoCodeSplitting: false }), react()],
  base: adminBase,
  /*
   * The two settings @hanzo/gui needs, and nothing more — the same arrangement
   * hanzo.sh and the console use. `@hanzo/ui` renders through gui's
   * cross-platform primitives, which resolve the react-native module by name on
   * web too, and `.web.*` first is what makes packages in that ecosystem hand
   * back their web variant instead of Flow source.
   */
  resolve: {
    alias: { '~': '/src', 'react-native': 'react-native-web' },
    extensions: ['.web.tsx', '.web.ts', '.web.jsx', '.web.js', '.mjs', '.js', '.mts', '.ts', '.jsx', '.tsx', '.json'],
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  server: {
    port: 3000,
    // Dev proxy to a local Base server. The admin talks to the rebranded /v1
    // API (BASE_API_PREFIX default). /v1/realtime is the SSE stream.
    proxy: {
      '/v1': {
        target: 'http://localhost:8090',
        changeOrigin: true,
        ws: true,
      },
    },
  },
})
