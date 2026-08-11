import { Link, Outlet } from '@tanstack/react-router'
import {
  Activity,
  Briefcase,
  Library,
  ListChecks,
  Menu,
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

// DashboardShell é o "chrome" da área administrativa (briefing 0083): sidebar
// fixa com as 6 seções (antes soltas no `AppHeader`) + `<Outlet/>` para a tela
// ativa. Mesmo padrão visual do `EntityEditShell` (0080) — nav com o item ativo
// destacado via `aria-current="page"` — e mesmo padrão responsivo do
// `AppHeader` (0075): abaixo de `md`, a sidebar vira um `Sheet` acionado por
// botão, em vez de espremer os 6 itens na tela.
const ITENS: {
  to:
    | '/dashboard/library'
    | '/dashboard/review'
    | '/dashboard/orphans'
    | '/dashboard/jobs'
    | '/dashboard/settings'
    | '/dashboard/webhook'
    | '/dashboard/health'
  label: string
  icon: LucideIcon
}[] = [
  { to: '/dashboard/library', label: 'Bibliotecas', icon: Library },
  { to: '/dashboard/review', label: 'Revisão', icon: ListChecks },
  { to: '/dashboard/orphans', label: 'Órfãs', icon: Trash2 },
  { to: '/dashboard/jobs', label: 'Jobs', icon: Briefcase },
  { to: '/dashboard/settings', label: 'Configurações', icon: Settings },
  { to: '/dashboard/webhook', label: 'Webhook', icon: Webhook },
  { to: '/dashboard/health', label: 'Saúde', icon: Activity },
]

// LinksSidebar é reaproveitado pelas DUAS renderizações (nav desktop em coluna
// e conteúdo do `Sheet` mobile) — mesmo motivo do `LinksNavegacao` do
// `AppHeader` (evitar duplicar os 6 links, 0075). `activeProps` vem do próprio
// TanStack Router: quando o `to` bate com a rota atual, ele soma
// `aria-current="page"` automaticamente (além da classe abaixo) — não é preciso
// calcular "ativo" à mão. `onNavigate` fecha o Sheet no mobile.
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
  // Abaixo do breakpoint `md` (768px), a sidebar some por trás de um `Sheet`
  // (mesmo padrão do `AppHeader`, 0075) — acima disso fica fixa à esquerda. O
  // estado fecha o Sheet ao navegar, senão o painel ficaria aberto sobre a
  // seção seguinte.
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
