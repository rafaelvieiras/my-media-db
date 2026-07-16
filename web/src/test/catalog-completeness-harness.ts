import type { CatalogCompletenessSettings } from '@/lib/api'
import { erroJson, okJson } from '@/test/detail-harness'

// completenessDefault é o default de fábrica devolvido pelo GET quando o dono nunca
// salvou nada (mesmo shape documentado no briefing 0057 §3/§6): description + poster +
// media_file marcados para movie/tv, resto desmarcado. Compartilhado entre os testes do
// formulário (catalog-completeness-form.test.tsx) e da tela de settings
// (settings.test.tsx) para não duplicarem a mesma fábrica (jscpd).
export const completenessDefault: CatalogCompletenessSettings = {
  movie: {
    description: true,
    runtime: false,
    certification: false,
    keywords: false,
    cast: false,
    media_file: true,
    artwork: ['poster'],
  },
  tv: {
    description: true,
    runtime: false,
    certification: false,
    keywords: false,
    cast: false,
    media_file: true,
    artwork: ['poster'],
  },
}

// respostaCompletude atende GET/PUT /api/v1/settings/catalog-completeness dado (url,
// method): devolve `null` quando a rota não casa, para o chamador tentar outra rota do
// seu próprio roteador de fetch. Compartilhado entre catalog-completeness-form.test.tsx
// (única rota) e settings.test.tsx (que combina esta com a rota de idiomas) — jscpd.
export function respostaCompletude(
  url: string,
  method: string,
  opts?: {
    getResposta?: CatalogCompletenessSettings
    putResposta?: CatalogCompletenessSettings
    putErro?: { status: number; code: string; message: string }
  },
): Response | null {
  if (url !== '/api/v1/settings/catalog-completeness') return null
  if (method === 'GET') {
    return okJson(opts?.getResposta ?? completenessDefault)
  }
  if (method === 'PUT') {
    if (opts?.putErro) {
      return erroJson(
        opts.putErro.status,
        opts.putErro.code,
        opts.putErro.message,
      )
    }
    return okJson(opts?.putResposta ?? completenessDefault)
  }
  return null
}
