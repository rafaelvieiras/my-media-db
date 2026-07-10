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
      // Alias usado pelo shadcn/ui e pelos imports internos.
      '@': path.resolve(__dirname, './src'),
    },
  },
  // Config do vitest (a chave `test` é ignorada pelo `vite build`, então não
  // afeta o bundle de produção). Reaproveita o alias `@` acima.
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
    // Descobre só os nossos testes (o `node_modules_rootbak` — backup ignorado
    // — contém testes de terceiros que não devem rodar).
    include: ['src/**/*.test.{ts,tsx}'],
    exclude: ['**/node_modules/**', '**/node_modules_rootbak/**', '**/dist/**'],
    coverage: {
      provider: 'v8',
      reporter: ['text', 'json-summary'],
      reportsDirectory: './coverage',
      // No vitest 4 a contagem de arquivos-fonte NÃO cobertos é o padrão (não há
      // mais a flag `all`): todo arquivo casado por `include` entra no cálculo,
      // mesmo sem teste — baseline honesto (ex.: router.tsx conta como 0%).
      include: ['src/**'],
      exclude: [
        // Ponto de entrada (só monta o React na DOM) — sem lógica testável.
        'src/main.tsx',
        // Componentes shadcn/ui são GERADOS — não são lógica nossa.
        'src/components/ui/**',
        // api-types.ts é GERADO do openapi.yaml (npm run gen:types) — só tipos,
        // sem código executável; fora da cobertura como os componentes gerados.
        'src/lib/api-types.ts',
        // Infra de teste e os próprios testes.
        'src/test/**',
        'src/**/*.test.{ts,tsx}',
        // Declarações de tipo e estilos (não são código executável).
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
      // Em dev o front chama caminho relativo (/api/...) e o Vite encaminha
      // para o backend Go — mesmo contrato de produção, sem CORS.
      '/api': {
        // Alvo configurável: `npm run dev` local usa localhost:8080; no compose
        // de dev (0004) o serviço `web` define VITE_PROXY_TARGET=http://api:8080
        // porque `localhost` dentro do container não é o serviço `api`.
        target: process.env.VITE_PROXY_TARGET || 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
})
