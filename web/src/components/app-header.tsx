import { Link } from '@tanstack/react-router'
import {
  Briefcase,
  ListChecks,
  Library,
  Menu,
  Search,
  Settings,
  Trash2,
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
import { LanguageSelector } from '@/components/language-selector'

// Cabeçalho com navegação entre as telas da Fase 2. `activeProps` destaca a rota
// atual (type-safe pelo router code-based).
const linkBase =
  'rounded-lg px-2.5 py-1 text-sm font-medium text-muted-foreground transition-colors hover:text-foreground'
const linkAtivo = 'bg-muted text-foreground'

// NavLink fatora o padrão repetido dos links de navegação (className condicional
// ao ícone + activeProps) — cada entrada abaixo era um <Link> quase idêntico,
// o que o jscpd passou a acusar como duplicação ao crescer para 7 entradas.
// `onNavigate` é usado só na cópia dentro do menu mobile (0075), para fechar o
// `Sheet` ao clicar num link — no desktop fica `undefined` e não muda nada.
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

// Lista de navegação reaproveitada pelas DUAS renderizações (nav desktop em
// linha e conteúdo do `Sheet` mobile) — evita duplicar os 7 `NavLink`s (0075).
// `onNavigate` fecha o Sheet no mobile; fica `undefined` (no-op) no desktop.
function LinksNavegacao({ onNavigate }: { onNavigate?: () => void }) {
  return (
    <>
      <NavLink to="/" exact onNavigate={onNavigate}>
        Biblioteca
      </NavLink>
      <NavLink to="/search" icon={Search} onNavigate={onNavigate}>
        Buscar
      </NavLink>
      <NavLink to="/library" onNavigate={onNavigate}>
        Bibliotecas
      </NavLink>
      <NavLink to="/review" icon={ListChecks} onNavigate={onNavigate}>
        Revisão
      </NavLink>
      <NavLink to="/orphans" icon={Trash2} onNavigate={onNavigate}>
        Órfãs
      </NavLink>
      {/* Administração: agrupa Configurações (idiomas) e Jobs (0052) — área de
      operação/config, não navegação do dia a dia. Um separador visual (borda +
      rótulo) basta para distingui-los dos links de navegação acima, sem
      precisar de um dropdown. */}
      <div
        role="group"
        aria-label="Administração"
        className="ml-1 flex items-center gap-1 border-l border-border pl-2 max-md:ml-0 max-md:w-full max-md:border-l-0 max-md:border-t max-md:pt-2 max-md:pl-0"
      >
        <span className="hidden text-xs text-muted-foreground sm:inline">
          Administração
        </span>
        <NavLink to="/settings" icon={Settings} onNavigate={onNavigate}>
          Configurações
        </NavLink>
        <NavLink to="/jobs" icon={Briefcase} onNavigate={onNavigate}>
          Jobs
        </NavLink>
      </div>
      <NavLink to="/health" onNavigate={onNavigate}>
        Saúde
      </NavLink>
    </>
  )
}

export function AppHeader() {
  // Abaixo do breakpoint `md` (768px), a nav some por trás de um `Sheet`
  // (0075) — acima disso continua exatamente a linha flex de sempre. O estado
  // controla o Sheet para fechá-lo ao navegar (senão o painel ficaria aberto
  // sobre a página seguinte, já que o `AppHeader` é fixo no layout raiz).
  const [menuAberto, setMenuAberto] = useState(false)
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
          <SheetTrigger asChild>
            <Button
              variant="ghost"
              size="icon"
              className="ml-auto md:hidden"
              aria-label="Abrir menu de navegação"
            >
              <Menu className="size-5" aria-hidden />
            </Button>
          </SheetTrigger>
          <SheetContent side="right" className="w-full max-w-xs gap-4 p-4">
            <SheetHeader className="p-0">
              <SheetTitle>Navegação</SheetTitle>
            </SheetHeader>
            <div className="flex flex-col items-start gap-1">
              <LinksNavegacao onNavigate={fecharMenu} />
            </div>
            <div className="mt-auto border-t border-border pt-3">
              <LanguageSelector />
            </div>
          </SheetContent>
        </Sheet>
      </nav>
    </header>
  )
}
