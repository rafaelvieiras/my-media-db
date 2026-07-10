import type { ReactNode } from 'react'

// Mock do <Link> do TanStack Router para testes de unidade de tela: vira um <a>
// simples, com o `to` refletido em `href` (para afirmar destinos de navegação).
// Quando `params` estão presentes (rotas com segmentos `$x`), interpola-os no
// `href` (ex.: `/external/$source/$type/$id` + `{source:'tmdb',…}` →
// `/external/tmdb/movie/604`), para os testes afirmarem o alvo já resolvido.
// Fica isolado num arquivo só-componente para não disparar
// `react-refresh/only-export-components` num módulo que também exporta helpers.
// Uso: `vi.mock('@tanstack/react-router', async () => ({ Link: (await
// import('@/test/link-mock')).LinkMock }))`.
export function LinkMock({
  children,
  className,
  to,
  params,
}: {
  children: ReactNode
  className?: string
  to?: string
  params?: Record<string, string>
}) {
  let href = to ?? '#'
  for (const [k, v] of Object.entries(params ?? {})) {
    href = href.replaceAll(`$${k}`, v)
  }
  return (
    <a href={href} className={className}>
      {children}
    </a>
  )
}
