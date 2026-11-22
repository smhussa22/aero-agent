import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

// dev: the app talks to the Go API through this proxy so the browser sees one origin
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: { '/api': { target: process.env.AERO_API ?? 'https://localhost:8080', secure: false } },
  },
  build: { sourcemap: false, target: 'es2022' },
  test: { environment: 'node', include: ['src/**/*.test.ts'] },
})
