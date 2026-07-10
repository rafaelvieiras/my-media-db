import type { ReactNode } from 'react'

// Mock do <Link> do TanStack Router para testes de unidade de tela: vira um <a>
// simples, com o `to` refletido em `href` (para afirmar destinos de navegação).
// Fica isolado num arquivo só-componente para não disparar
// `react-refresh/only-export-components` num módulo que também exporta helpers.
// Uso: `vi.mock('@tanstack/react-router', async () => ({ Link: (await
// import('@/test/link-mock')).LinkMock }))`.
export function LinkMock({
  children,
  className,
  to,
}: {
  children: ReactNode
  className?: string
  to?: string
}) {
  return (
    <a href={to ?? '#'} className={className}>
      {children}
    </a>
  )
}
