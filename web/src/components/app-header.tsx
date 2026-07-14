import { Link } from '@tanstack/react-router'
import {
  Briefcase,
  ListChecks,
  Library,
  Search,
  Settings,
  Trash2,
  type LucideIcon,
} from 'lucide-react'
import { LanguageSelector } from '@/components/language-selector'

// Cabeçalho com navegação entre as telas da Fase 2. `activeProps` destaca a rota
// atual (type-safe pelo router code-based).
const linkBase =
  'rounded-lg px-2.5 py-1 text-sm font-medium text-muted-foreground transition-colors hover:text-foreground'
const linkAtivo = 'bg-muted text-foreground'

// NavLink fatora o padrão repetido dos links de navegação (className condicional
// ao ícone + activeProps) — cada entrada abaixo era um <Link> quase idêntico,
// o que o jscpd passou a acusar como duplicação ao crescer para 7 entradas.
function NavLink({
  to,
  icon: Icon,
  exact,
  children,
}: {
  to: React.ComponentProps<typeof Link>['to']
  icon?: LucideIcon
  exact?: boolean
  children: React.ReactNode
}) {
  return (
    <Link
      to={to}
      activeOptions={exact ? { exact: true } : undefined}
      className={Icon ? `${linkBase} flex items-center gap-1.5` : linkBase}
      activeProps={{ className: linkAtivo }}
    >
      {Icon ? <Icon className="size-4" aria-hidden /> : null}
      {children}
    </Link>
  )
}

export function AppHeader() {
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
        <NavLink to="/" exact>
          Biblioteca
        </NavLink>
        <NavLink to="/search" icon={Search}>
          Buscar
        </NavLink>
        <NavLink to="/library">Bibliotecas</NavLink>
        <NavLink to="/review" icon={ListChecks}>
          Revisão
        </NavLink>
        <NavLink to="/orphans" icon={Trash2}>
          Órfãs
        </NavLink>
        {/* Administração: agrupa Configurações (idiomas) e Jobs (0052) — área de
        operação/config, não navegação do dia a dia. Um separador visual (borda +
        rótulo) basta para distingui-los dos links de navegação acima, sem
        precisar de um dropdown. */}
        <div
          role="group"
          aria-label="Administração"
          className="ml-1 flex items-center gap-1 border-l border-border pl-2"
        >
          <span className="hidden text-xs text-muted-foreground sm:inline">
            Administração
          </span>
          <NavLink to="/settings" icon={Settings}>
            Configurações
          </NavLink>
          <NavLink to="/jobs" icon={Briefcase}>
            Jobs
          </NavLink>
        </div>
        <NavLink to="/health">Saúde</NavLink>
        <div className="ml-auto">
          <LanguageSelector />
        </div>
      </nav>
    </header>
  )
}
