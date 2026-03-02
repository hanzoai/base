import { TanStackRouterVite } from '@tanstack/router-plugin/vite';
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

// Vite 8 + React 19 + TanStack Router file-based routing.
// Output goes to `dist/` which the Go binary embeds via embed.go.
export default defineConfig({
  plugins: [
    TanStackRouterVite({ target: 'react', autoCodeSplitting: true }),
    react(),
  ],
  base: '/_/', // Admin UI is mounted at /_/ on the Base server
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: true,
    // Single bundle for simpler go:embed; tree-shaking handles size
    rollupOptions: {
      output: {
        manualChunks: undefined,
      },
    },
  },
  server: {
    port: 3000,
    // Proxy all non-UI paths to the Base Go server running on :8090
    proxy: {
      '/api': 'http://localhost:8090',
      '/realtime': {
        target: 'http://localhost:8090',
        changeOrigin: true,
        ws: true,
      },
    },
  },
});
