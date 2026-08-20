import { Link } from '@tanstack/react-router'
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
import { entityTypesQueryOptions } from '@/lib/queries'
import { rotuloTipoPlural } from '@/lib/entity-badges'
import { tipoParaSlug } from '@/lib/library-slugs'

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

// BibliotecaNavItem (briefing 0110, atualizado no 0114/ADR-0030) combina o
// link direto de sempre ("Biblioteca" → `/` sem filtro, comportamento
// preservado) com um `DropdownMenu` separado (trigger = chevron ao lado do
// texto) que lista os schemas com dado catalogado (`entityTypesQueryOptions`,
// mesma fonte do carrossel da home, 0109) e navega direto pra rota dedicada
// `/library/$entitySlug` daquele tipo (0114 — antes era `?type=X` na própria
// `/`, que não existe mais). Slug resolvido por `tipoParaSlug`
// (`library-slugs.ts`, mesmo mapa do link "Ver Todos" do carrossel); um tipo
// sem slug mapeado (defensivo) some do dropdown em vez de virar link quebrado.
// Trigger PRÓPRIO (não o texto inteiro) porque o texto precisa continuar
// navegando direto sem abrir menu — decisão explícita do briefing ("garanta
// que ainda dá pra ir pra biblioteca sem filtro tão fácil quanto hoje"). O
// `DropdownMenu` do Radix abre por clique/teclado (não hover), então funciona
// igual por toque dentro do `Sheet` mobile sem tratamento especial.
function BibliotecaNavItem({ onNavigate }: { onNavigate?: () => void }) {
  const { data: tipos } = useQuery(entityTypesQueryOptions)
  const itensComSlug = (tipos ?? []).flatMap((tipo) => {
    const slug = tipoParaSlug(tipo.type)
    return slug ? [{ type: tipo.type, slug }] : []
  })

  return (
    <div className="flex items-center">
      <NavLink to="/" exact onNavigate={onNavigate}>
        Biblioteca
      </NavLink>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size="icon"
            className="size-6 shrink-0 text-muted-foreground hover:text-foreground"
            aria-label="Filtrar biblioteca por tipo"
          >
            <ChevronDown className="size-3.5" aria-hidden />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start">
          {itensComSlug.map(({ type, slug }) => (
            <DropdownMenuItem key={type} asChild onSelect={onNavigate}>
              <Link to="/library/$entitySlug" params={{ entitySlug: slug }}>
                {rotuloTipoPlural(type)}
              </Link>
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}

// Lista de navegação reaproveitada pelas DUAS renderizações (nav desktop em
// linha e conteúdo do `Sheet` mobile) — evita duplicar os `NavLink`s (0075).
// `onNavigate` fecha o Sheet no mobile; fica `undefined` (no-op) no desktop.
// As 6 telas de operação/config (Bibliotecas/Revisão/Órfãs/Jobs/Configurações/
// Saúde) saíram daqui para a sidebar do `DashboardShell` (briefing 0083) — só
// resta o link único "Dashboard" para a área administrativa.
function LinksNavegacao({ onNavigate }: { onNavigate?: () => void }) {
  return (
    <>
      <BibliotecaNavItem onNavigate={onNavigate} />
      <NavLink to="/search" icon={Search} onNavigate={onNavigate}>
        Buscar
      </NavLink>
      <NavLink to="/dashboard" icon={LayoutDashboard} onNavigate={onNavigate}>
        Dashboard
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
