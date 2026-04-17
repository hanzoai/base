import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [react()],
  base: '/_/',
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
