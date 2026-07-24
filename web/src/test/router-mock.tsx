import type { MouseEventHandler, ReactNode } from 'react'

// LinkMock substitui `<Link>` do TanStack Router nos testes que mockam o módulo
// inteiro (`vi.mock('@tanstack/react-router', ...)`): repassa `onClick` (guard
// de "sair sem salvar", 0073/0080) e `aria-current` (seção ativa da sidebar de
// edição, 0080) — as duas props que essas suítes precisam inspecionar.
// Compartilhado pelas telas de edição de item para não duplicarem o mesmo mock
// (jscpd).
export function LinkMock({
  children,
  onClick,
  'aria-current': ariaCurrent,
}: {
  children: ReactNode
  onClick?: MouseEventHandler<HTMLAnchorElement>
  'aria-current'?: 'page'
}) {
  return (
    <a href="#" onClick={onClick} aria-current={ariaCurrent}>
      {children}
    </a>
  )
}
