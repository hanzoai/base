import { defineConfig } from 'tsup'

export default defineConfig({
  entry: {
    'core/index': 'src/core/index.ts',
    'react/index': 'src/react/index.ts',
    'crdt/index': 'src/crdt/index.ts',
  },
  format: ['esm'],
  dts: true,
  sourcemap: true,
  clean: true,
  splitting: true,
  treeshake: true,
  target: 'es2020',
  outDir: 'dist',
  external: ['react'],
})
