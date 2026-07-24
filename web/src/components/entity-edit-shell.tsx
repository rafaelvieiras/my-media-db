import type { MouseEvent, ReactNode } from 'react'
import { useEffect } from 'react'
import { Link, useNavigate } from '@tanstack/react-router'
import { ArrowLeft } from 'lucide-react'
import { PosterImg } from '@/components/rich-detail-view'
import { LanguageSelector } from '@/components/language-selector'
import { arteSrc, type ResolvedEntity } from '@/lib/api'
import { cn } from '@/lib/utils'

// Seção de edição (briefing 0080): as TRÊS únicas seções que o my-media-db tem
// como dado editável de verdade hoje — nada além disso (ver o briefing para o
// porquê de não replicar as ~13 seções do editor do TMDB).
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

// EntityEditShell é o "chrome" compartilhado pelas 3 subrotas de edição (briefing
// 0080): cabeçalho (pôster+título+idioma) + sidebar de navegação entre seções +
// "Voltar ao item". Cada subrota (`entity-edit-basico.tsx`/`-ids.tsx`/
// `-creditos.tsx`) monta só o bloco correspondente como `children` e passa o
// `sujo` daquele bloco só — o guard de "sair sem salvar" (0073) vive aqui,
// simplificado: não precisa mais combinar 3 booleanos, porque cada subrota só
// tem 1 bloco por vez.
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
  // Guard de "sair sem salvar" (0073): "sujo" da seção ATUAL só — cada subrota
  // monta 1 bloco, então não há mais estado de 3 blocos para combinar.
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

  // guardarClique intercepta o clique de QUALQUER link de navegação para fora da
  // seção atual (outra seção da sidebar OU "Voltar ao item"): se a seção atual
  // estiver suja, confirma antes de navegar — mesmo comportamento que o 0073 já
  // tinha para "Voltar ao item", agora reaproveitado para os 3 links da sidebar.
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
