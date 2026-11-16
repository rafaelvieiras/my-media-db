import type { MouseEvent, ReactNode } from 'react'
import { useEffect } from 'react'
import { Link, useNavigate } from '@tanstack/react-router'
import { ArrowLeft } from 'lucide-react'
import { PosterImg } from '@/components/rich-detail-view'
import { LanguageSelector } from '@/components/language-selector'
import { arteSrc, type ResolvedEntity } from '@/lib/api'
import { cn } from '@/lib/utils'

// Edit section: the THREE only sections that my-media-db has
// as truly editable data today — nothing beyond that (the reasoning for not
// replicating the ~13 sections of the TMDB editor is documented in the edit spec).
export type SecaoEdicao = 'basico' | 'ids' | 'creditos'

const SECOES: {
  secao: SecaoEdicao
  to:
    | '/entities/$id/edit'
    | '/entities/$id/edit/ids'
    | '/entities/$id/edit/creditos'
  label: string
}[] = [
  { secao: 'basico', to: '/entities/$id/edit', label: 'Informações básicas' },
  { secao: 'ids', to: '/entities/$id/edit/ids', label: 'IDs de terceiros' },
  {
    secao: 'creditos',
    to: '/entities/$id/edit/creditos',
    label: 'Elenco e equipe',
  },
]

// EntityEditShell is the "chrome" shared by the 3 edit subroutes:
// header (poster+title+language) + navigation sidebar between sections +
// "Voltar ao item". Each subroute (`entity-edit-basico.tsx`/`-ids.tsx`/
// `-creditos.tsx`) mounts only the corresponding block as `children` and passes the
// `sujo` state of that block only — the "leave without saving" guard lives here,
// simplified: it no longer needs to combine 3 booleans, because each subroute has only
// 1 block at a time.
export function EntityEditShell({
  id,
  entity,
  secaoAtiva,
  sujo,
  children,
}: {
  id: string
  entity: ResolvedEntity
  secaoAtiva: SecaoEdicao
  // "Leave without saving" guard: the "dirty" state of the CURRENT section only — each subroute
  // mounts 1 block, so there is no longer a state of 3 blocks to combine.
  sujo: boolean
  children: ReactNode
}) {
  const navigate = useNavigate()

  useEffect(() => {
    if (!sujo) return
    function aoFechar(e: BeforeUnloadEvent) {
      e.preventDefault()
      e.returnValue = ''
    }
    window.addEventListener('beforeunload', aoFechar)
    return () => window.removeEventListener('beforeunload', aoFechar)
  }, [sujo])

  // guardarClique intercepts the click of ANY navigation link out of the
  // current section (another section of the sidebar OR "Voltar ao item"): if the current section
  // is dirty, it confirms before navigating — the same behavior the guard already
  // had for "Voltar ao item", now reused for the 3 links of the sidebar.
  function guardarClique(navegar: () => void) {
    return (e: MouseEvent<HTMLAnchorElement>) => {
      if (!sujo) return
      e.preventDefault()
      if (
        window.confirm(
          'Você tem alterações não salvas neste item. Sair mesmo assim?',
        )
      ) {
        navegar()
      }
    }
  }

  return (
    <main className="mx-auto flex max-w-5xl flex-col gap-6 px-4 py-6">
      <Link
        to="/entities/$id"
        params={{ id }}
        onClick={guardarClique(() =>
          navigate({ to: '/entities/$id', params: { id } }),
        )}
        className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
      >
        <ArrowLeft className="size-4" aria-hidden />
        Voltar ao item
      </Link>

      <header className="flex flex-wrap items-center gap-4">
        <PosterImg
          src={arteSrc(entity.poster_url)}
          alt={`Pôster de ${entity.title}`}
          className="w-16 shrink-0"
        />
        <h1 className="min-w-0 flex-1 truncate font-heading text-xl font-semibold">
          {entity.title}
        </h1>
        <LanguageSelector />
      </header>

      <div className="flex flex-col gap-6 sm:flex-row">
        <nav
          aria-label="Seções de edição"
          className="flex shrink-0 flex-row gap-1 overflow-x-auto sm:w-48 sm:flex-col sm:overflow-visible"
        >
          {SECOES.map((item) => {
            const ativa = item.secao === secaoAtiva
            return (
              <Link
                key={item.secao}
                to={item.to}
                params={{ id }}
                onClick={guardarClique(() =>
                  navigate({ to: item.to, params: { id } }),
                )}
                aria-current={ativa ? 'page' : undefined}
                className={cn(
                  'rounded-md px-3 py-2 text-sm whitespace-nowrap',
                  ativa
                    ? 'bg-secondary font-medium text-secondary-foreground'
                    : 'text-muted-foreground hover:bg-secondary/50 hover:text-foreground',
                )}
              >
                {item.label}
              </Link>
            )
          })}
        </nav>

        <div className="min-w-0 flex-1">{children}</div>
      </div>
    </main>
  )
}
