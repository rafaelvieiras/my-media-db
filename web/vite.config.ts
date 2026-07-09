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
  build: {
    // Gera exatamente onde o backend Go embute o SPA via //go:embed.
    outDir: '../internal/webui/dist',
    emptyOutDir: true,
  },
  server: {
    strictPort: true,
    proxy: {
      // Em dev o front chama caminho relativo (/api/...) e o Vite encaminha
      // para o backend Go — mesmo contrato de produção, sem CORS.
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
})
