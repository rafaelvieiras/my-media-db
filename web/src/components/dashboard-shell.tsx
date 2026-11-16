import { Link, Outlet } from '@tanstack/react-router'
import {
  Activity,
  Briefcase,
  FileQuestion,
  Library,
  ListChecks,
  Menu,
  Plug,
  Settings,
  Trash2,
  Webhook,
  type LucideIcon,
} from 'lucide-react'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '@/components/ui/sheet'

// DashboardShell is the "chrome" of the administrative area: a fixed
// sidebar with the 6 sections (before they were loose in the `AppHeader`) + `<Outlet/>` for the
// active screen. Same visual pattern as `EntityEditShell` — nav with the active item
// highlighted via `aria-current="page"` — and same responsive pattern as
// `AppHeader`: below `md`, the sidebar becomes a `Sheet` triggered by a
// button, instead of squeezing the 6 items on the screen.
const ITENS: {
  to:
    | '/dashboard/library'
    | '/dashboard/review'
    | '/dashboard/orphans'
    | '/dashboard/jobs'
    | '/dashboard/settings'
    | '/dashboard/integrations'
    | '/dashboard/webhook'
    | '/dashboard/historico-orfaos'
    | '/dashboard/health'
  label: string
  icon: LucideIcon
}[] = [
  { to: '/dashboard/library', label: 'Bibliotecas', icon: Library },
  { to: '/dashboard/review', label: 'Revisão', icon: ListChecks },
  { to: '/dashboard/orphans', label: 'Órfãs', icon: Trash2 },
  { to: '/dashboard/jobs', label: 'Jobs', icon: Briefcase },
  { to: '/dashboard/settings', label: 'Configurações', icon: Settings },
  // Integrações: between Configurações and Webhook -- the same area
  // of "provider credentials/config", before the more operational areas
  // (Webhook/Eventos sem item/Saúde).
  { to: '/dashboard/integrations', label: 'Integrações', icon: Plug },
  { to: '/dashboard/webhook', label: 'Webhook', icon: Webhook },
  {
    to: '/dashboard/historico-orfaos',
    label: 'Eventos sem item',
    icon: FileQuestion,
  },
  { to: '/dashboard/health', label: 'Saúde', icon: Activity },
]

// LinksSidebar is reused by BOTH renderings (desktop nav in a column
// and content of the mobile `Sheet`) — same reason as `LinksNavegacao` of
// the `AppHeader` (avoid duplicating the 6 links). `activeProps` comes from the
// TanStack Router itself: when the `to` matches the current route, it adds
// `aria-current="page"` automatically (besides the class below) — there is no need to
// compute "active" by hand. `onNavigate` closes the Sheet on mobile.
function LinksSidebar({ onNavigate }: { onNavigate?: () => void }) {
  return (
    <>
      {ITENS.map(({ to, label, icon: Icon }) => (
        <Link
          key={to}
          to={to}
          onClick={onNavigate}
          activeProps={{
            className: 'bg-secondary font-medium text-secondary-foreground',
          }}
          className="flex items-center gap-2 rounded-md px-3 py-2 text-sm whitespace-nowrap text-muted-foreground hover:bg-secondary/50 hover:text-foreground"
        >
          <Icon className="size-4" aria-hidden />
          {label}
        </Link>
      ))}
    </>
  )
}

export function DashboardShell() {
  // Below the `md` breakpoint (768px), the sidebar disappears behind a `Sheet`
  // (same pattern as the `AppHeader`) — above it, it stays fixed on the left. The
  // state closes the Sheet on navigation, otherwise the panel would stay open over the
  // next section.
  const [menuAberto, setMenuAberto] = useState(false)
  const fecharMenu = () => setMenuAberto(false)

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6 px-4 py-6 md:flex-row">
      <div className="flex items-center justify-between md:hidden">
        <h1 className="font-heading text-lg font-semibold">Administração</h1>
        <Sheet open={menuAberto} onOpenChange={setMenuAberto}>
          <SheetTrigger asChild>
            <Button
              variant="outline"
              size="icon"
              className="md:hidden"
              aria-label="Abrir menu de administração"
            >
              <Menu className="size-5" aria-hidden />
            </Button>
          </SheetTrigger>
          <SheetContent side="left" className="w-full max-w-xs gap-4 p-4">
            <SheetHeader className="p-0">
              <SheetTitle>Administração</SheetTitle>
            </SheetHeader>
            <div className="flex flex-col items-start gap-1">
              <LinksSidebar onNavigate={fecharMenu} />
            </div>
          </SheetContent>
        </Sheet>
      </div>

      <nav
        aria-label="Administração"
        className="hidden shrink-0 flex-col gap-1 md:flex md:w-52"
      >
        <LinksSidebar />
      </nav>

      <main className="min-w-0 flex-1">
        <Outlet />
      </main>
    </div>
  )
}
