import type { MouseEventHandler, ReactNode } from 'react'

// LinkMock replaces the `<Link>` of TanStack Router in the tests that mock the whole
// module (`vi.mock('@tanstack/react-router', ...)`): it forwards `onClick` (the
// "leave without saving" guard) and `aria-current` (active section of the edit
// sidebar) — the two props those suites need to inspect.
// Shared by the item edit screens so they do not duplicate the same mock
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
