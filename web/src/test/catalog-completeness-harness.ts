import type { CatalogCompletenessSettings } from '@/lib/api'
import { erroJson, okJson } from '@/test/detail-harness'

// completenessDefault is the factory default returned by the GET when the owner never
// saved anything (same shape documented in §3/§6): description + poster +
// media_file marked for movie/tv, the rest unmarked. Shared between the tests of the
// form (catalog-completeness-form.test.tsx) and of the settings screen
// (settings.test.tsx) so they do not duplicate the same factory (jscpd).
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

// respostaCompletude answers GET/PUT /api/v1/settings/catalog-completeness for the given (url,
// method): it returns `null` when the route does not match, so the caller can try another route of
// its own fetch router. Shared between catalog-completeness-form.test.tsx
// (single route) and settings.test.tsx (which combines this one with the languages route) — jscpd.
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
