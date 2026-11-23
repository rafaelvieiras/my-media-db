import { Link, useLocation } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import {
  ChevronDown,
  LayoutDashboard,
  Library,
  Menu,
  Search,
  type LucideIcon,
} from 'lucide-react'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '@/components/ui/sheet'
import { LanguageSelector } from '@/components/language-selector'
import { useT } from '@/lib/i18n'
import { entityTypesQueryOptions } from '@/lib/queries'
import { rotuloTipoPlural } from '@/lib/entity-badges'
import { tipoParaSlug } from '@/lib/library-slugs'

// Header with navigation between the Phase 2 screens. `activeProps` highlights the current
// route (type-safe through the code-based router).
// The colour lives in TWO disjoint classes on purpose: the base must not carry
// `text-muted-foreground`, otherwise the active pill (bg + colour) loses the colour cascade and
// the current page looks exactly like the others (measured: pill 1.09:1, text 4.35:1, same as
// inactive). The active state now uses the same `secondary` pair as the admin sidebar.
const linkBase =
  'rounded-lg px-2.5 py-1 text-sm font-medium transition-colors text-muted-foreground hover:text-foreground'
const linkAtivo = 'bg-secondary font-semibold text-secondary-foreground'

// NavLink factors out the repeated pattern of the navigation links (conditional className
// on the icon + activeProps) — each entry below used to be an almost identical <Link>,
// which jscpd started flagging as duplication once it grew to 7 entries.
// `onNavigate` is used only in the copy inside the mobile menu, to close the
// `Sheet` when a link is clicked — on desktop it stays `undefined` and changes nothing.
function NavLink({
  to,
  icon: Icon,
  exact,
  onNavigate,
  children,
}: {
  to: React.ComponentProps<typeof Link>['to']
  icon?: LucideIcon
  exact?: boolean
  onNavigate?: () => void
  children: React.ReactNode
}) {
  return (
    <Link
      to={to}
      activeOptions={exact ? { exact: true } : undefined}
      className={Icon ? `${linkBase} flex items-center gap-1.5` : linkBase}
      activeProps={{ className: linkAtivo }}
      onClick={onNavigate}
    >
      {Icon ? <Icon className="size-4" aria-hidden /> : null}
      {children}
    </Link>
  )
}

// BibliotecaNavItem (updated in ADR-0030) combines the
// the usual direct link ("Biblioteca" → `/` without a filter, behavior
// preserved) with a separate `DropdownMenu` (trigger = chevron next to the
// text) that lists the schemas with cataloged data (`entityTypesQueryOptions`,
// same source as the home carousel) and navigates straight to the dedicated
// route `/library/$entitySlug` of that type (before it was `?type=X` on `/`
// itself, which no longer exists). The slug is resolved by `tipoParaSlug`
// (`library-slugs.ts`, the same map as the "Ver Todos" link of the carousel); a type
// without a mapped slug (defensive) disappears from the dropdown instead of becoming a broken link.
// Its OWN trigger (not the whole text) because the text has to keep
// navigating directly without opening a menu — an explicit decision ("make
// sure you can still get to the library without a filter as easily as today"). The
// Radix `DropdownMenu` opens by click/keyboard (not hover), so it works
// the same way by touch inside the mobile `Sheet` without special handling.
function BibliotecaNavItem({ onNavigate }: { onNavigate?: () => void }) {
  const t = useT()
  const { data: tipos } = useQuery(entityTypesQueryOptions)
  const itensComSlug = (tipos ?? []).flatMap((tipo) => {
    const slug = tipoParaSlug(tipo.type)
    return slug ? [{ type: tipo.type, slug }] : []
  })

  return (
    <div className="flex items-center">
      <NavLink to="/" exact onNavigate={onNavigate}>
        {t('nav.header.library')}
      </NavLink>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size="icon"
            className="size-11 shrink-0 text-muted-foreground hover:text-foreground sm:size-6"
            aria-label={t('nav.header.filterByType')}
          >
            <ChevronDown className="size-3.5" aria-hidden />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start">
          {itensComSlug.map(({ type, slug }) => (
            <DropdownMenuItem key={type} asChild onSelect={onNavigate}>
              <Link to="/library/$entitySlug" params={{ entitySlug: slug }}>
                {rotuloTipoPlural(type, t)}
              </Link>
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}

// Navigation list reused by BOTH renderings (desktop inline nav and
// content of the mobile `Sheet`) — avoids duplicating the `NavLink`s.
// `onNavigate` closes the Sheet on mobile; it stays `undefined` (no-op) on desktop.
// The 6 operation/config screens (Bibliotecas/Revisão/Órfãs/Jobs/Configurações/
// Saúde) moved from here to the sidebar of the `DashboardShell` — only
// the single "Dashboard" link to the administrative area is left.
function LinksNavegacao({ onNavigate }: { onNavigate?: () => void }) {
  const t = useT()
  return (
    <>
      <BibliotecaNavItem onNavigate={onNavigate} />
      <NavLink to="/search" icon={Search} onNavigate={onNavigate}>
        {t('nav.header.search')}
      </NavLink>
      <NavLink to="/dashboard" icon={LayoutDashboard} onNavigate={onNavigate}>
        {t('nav.header.dashboard')}
      </NavLink>
    </>
  )
}

export function AppHeader() {
  const t = useT()
  // Below the `md` breakpoint (768px), the nav disappears behind a `Sheet`
  // — above it, it stays exactly the usual flex row. The state
  // controls the Sheet so it closes on navigation (otherwise the panel would stay open
  // over the next page, since the `AppHeader` is fixed in the root layout).
  const [menuAberto, setMenuAberto] = useState(false)
  const { pathname } = useLocation()
  const noAdmin = pathname.startsWith('/dashboard')
  // The sheet element itself, so the language menu can portal INSIDE it (see
  // LanguageSelector's `container`). A callback ref keeps it in state, so the menu re-renders
  // with the real element once it mounts.
  const [sheetEl, setSheetEl] = useState<HTMLDivElement | null>(null)
  const fecharMenu = () => setMenuAberto(false)

  return (
    <header className="sticky top-0 z-10 border-b bg-background/80 backdrop-blur">
      <nav className="mx-auto flex max-w-6xl items-center gap-1 px-4 py-2">
        <Link
          to="/"
          className="mr-2 flex items-center gap-2 font-heading font-semibold"
        >
          <Library className="size-5" aria-hidden />
          MyMediaDB
        </Link>
        <div className="hidden min-w-0 flex-1 items-center gap-1 md:flex">
          <LinksNavegacao />
          <div className="ml-auto">
            <LanguageSelector />
          </div>
        </div>
        <Sheet open={menuAberto} onOpenChange={setMenuAberto}>
          {/* Inside the admin area the DashboardShell owns the mobile navigation (its own
              button + its own sheet, with a way back to the catalog). Two hamburgers stacked
              110 px apart, each opening a different menu, was the measured result. */}
          <SheetTrigger asChild>
            <Button
              variant="ghost"
              size="icon"
              className={`ml-auto md:hidden ${noAdmin ? 'hidden' : ''}`}
              aria-label={t('nav.header.openMenu')}
            >
              <Menu className="size-5" aria-hidden />
            </Button>
          </SheetTrigger>
          <SheetContent
            ref={setSheetEl}
            side="right"
            className="w-full max-w-xs gap-4 p-4"
          >
            <SheetHeader className="p-0">
              <SheetTitle>{t('nav.header.menuTitle')}</SheetTitle>
            </SheetHeader>
            <div className="flex flex-col items-start gap-1">
              <LinksNavegacao onNavigate={fecharMenu} />
            </div>
            <div className="mt-auto border-t border-border pt-3">
              <LanguageSelector container={sheetEl} />
            </div>
          </SheetContent>
        </Sheet>
      </nav>
    </header>
  )
}
