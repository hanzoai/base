import { TanStackRouterVite } from '@tanstack/router-plugin/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

export default defineConfig({
  // Router plugin generates src/routeTree.gen.ts from src/routes/** (gitignored).
  plugins: [TanStackRouterVite({ target: 'react', autoCodeSplitting: false }), react()],
  // The admin is the site: base.hanzo.ai serves it at the root, and the API
  // answers under /v1. Nothing to keep in agreement with a server path or a
  // redirect URI registered with IAM.
  base: '/',
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
