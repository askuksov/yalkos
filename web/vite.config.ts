/// <reference types="vitest/config" />
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// Go server: `make dev` sets VITE_PROXY_TARGET (http://app:8080) inside Docker;
// when Vite runs on the host it falls back to the port from YALKOS_PORT.
const backend = process.env.VITE_PROXY_TARGET ?? `http://localhost:${process.env.YALKOS_PORT ?? '8080'}`

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': backend,
      '/graphql': backend,
      '/healthz': backend,
      '/readyz': backend,
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  test: {
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
  },
})
