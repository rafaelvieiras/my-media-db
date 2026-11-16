import type { ReactNode } from 'react'

// Mock of the <Link> of TanStack Router for screen unit tests: it becomes a plain
// <a>, with `to` reflected in `href` (to assert navigation destinations).
// When `params` are present (routes with `$x` segments), it interpolates them into
// the `href` (e.g. `/external/$source/$type/$id` + `{source:'tmdb',…}` →
// `/external/tmdb/movie/604`), so the tests can assert the already resolved target.
// `search` (e.g. `{type:'album'}`) becomes a query string in the `href`
// (`?type=album`) — same logic, keys with an `undefined` value are omitted.
// It stays isolated in a single-component file so it does not trigger
// `react-refresh/only-export-components` in a module that also exports helpers.
// Usage: `vi.mock('@tanstack/react-router', async () => ({ Link: (await
// import('@/test/link-mock')).LinkMock }))`.
export function LinkMock({
  children,
  className,
  to,
  params,
  search,
  ...rest
}: {
  children: ReactNode
  className?: string
  to?: string
  params?: Record<string, string>
  search?: Record<string, string | undefined>
} & Record<string, unknown>) {
  let href = to ?? '#'
  for (const [k, v] of Object.entries(params ?? {})) {
    href = href.replaceAll(`$${k}`, v)
  }
  if (search) {
    const qs = new URLSearchParams()
    for (const [k, v] of Object.entries(search)) {
      if (v !== undefined) {
        qs.set(k, v)
      }
    }
    const qsStr = qs.toString()
    if (qsStr) {
      href += `?${qsStr}`
    }
  }
  return (
    <a href={href} className={className} {...rest}>
      {children}
    </a>
  )
}
