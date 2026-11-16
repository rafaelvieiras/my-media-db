/// <reference types="vitest/config" />
import path from 'node:path'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      // Alias used by shadcn/ui and by the internal imports.
      '@': path.resolve(__dirname, './src'),
    },
  },
  // vitest config (the `test` key is ignored by `vite build`, so it does not
  // affect the production bundle). It reuses the `@` alias above.
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
    // Finds only our own tests (the ignored backup `node_modules_rootbak`
    // holds third-party tests that must not run).
    include: ['src/**/*.test.{ts,tsx}'],
    exclude: ['**/node_modules/**', '**/node_modules_rootbak/**', '**/dist/**'],
    coverage: {
      provider: 'v8',
      reporter: ['text', 'json-summary'],
      reportsDirectory: './coverage',
      // In vitest 4 counting uncovered source files is the default (the `all`
      // flag is gone): every file matched by `include` enters the math, even
      // without a test, which keeps the baseline honest (router.tsx counts 0%).
      include: ['src/**'],
      exclude: [
        // Entry point (it only mounts React into the DOM), no testable logic.
        'src/main.tsx',
        // shadcn/ui components are GENERATED, not our logic.
        'src/components/ui/**',
        // api-types.ts is GENERATED from openapi.yaml (npm run gen:types): types
        // only, no executable code, so it stays out of coverage like the generated
        // components.
        'src/lib/api-types.ts',
        // Test infrastructure and the tests themselves.
        'src/test/**',
        'src/**/*.test.{ts,tsx}',
        // Type declarations and styles (not executable code).
        'src/**/*.d.ts',
        'src/**/*.css',
      ],
    },
  },
  build: {
    // Gera exatamente onde o backend Go embute o SPA via //go:embed.
    outDir: '../internal/webui/dist',
    emptyOutDir: true,
    // Sourcemaps INLINE só no build de cobertura E2E (E2E_COVERAGE=1): o monocart lê
    // o mapa embutido no próprio bundle e mapeia a cobertura V8 do Chromium de volta
    // ao TSX de origem (Camada 2, briefing 0028). Em produção fica false (sem mapa).
    sourcemap: process.env.E2E_COVERAGE === '1' ? 'inline' : false,
  },
  server: {
    strictPort: true,
    proxy: {
      // In dev the front end calls a relative path (/api/...) and Vite forwards
      // it to the Go backend: same contract as production, no CORS.
      '/api': {
        // Configurable target: a local `npm run dev` uses localhost:8080; in the
        // dev compose the `web` service sets VITE_PROXY_TARGET=http://api:8080,
        // because `localhost` inside the container is not the `api` service.
        target: process.env.VITE_PROXY_TARGET || 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
})
