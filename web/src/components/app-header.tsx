import { Link } from '@tanstack/react-router'
import { Library, Search } from 'lucide-react'

// Cabeçalho com navegação entre as telas da Fase 2. `activeProps` destaca a rota
// atual (type-safe pelo router code-based).
const linkBase =
  'rounded-lg px-2.5 py-1 text-sm font-medium text-muted-foreground transition-colors hover:text-foreground'
const linkAtivo = 'bg-muted text-foreground'

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
        <Link
          to="/"
          activeOptions={{ exact: true }}
          className={linkBase}
          activeProps={{ className: linkAtivo }}
        >
          Biblioteca
        </Link>
        <Link
          to="/search"
          className={`${linkBase} flex items-center gap-1.5`}
          activeProps={{ className: linkAtivo }}
        >
          <Search className="size-4" aria-hidden />
          Buscar
        </Link>
        <Link
          to="/library"
          className={linkBase}
          activeProps={{ className: linkAtivo }}
        >
          Bibliotecas
        </Link>
        <Link
          to="/health"
          className={linkBase}
          activeProps={{ className: linkAtivo }}
        >
          Saúde
        </Link>
      </nav>
    </header>
  )
}
