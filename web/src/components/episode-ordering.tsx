import type { ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Select } from '@/components/ui/select'
import { CardNumerado, GradeMembros } from '@/components/entity-members'
import { ErrorState, LoadingState } from '@/components/query-states'
import { useLanguage } from '@/lib/language-context'
import { useT } from '@/lib/i18n'
import type { Translate } from '@/lib/i18n/types'
import { entityEpisodesQueryOptions } from '@/lib/queries'

// Episode ordering selector: on the series screen, it switches the display
// order (official/dvd/absolute…). The default order shows the navigable season
// cards (the current view); alternative orders regroup the episodes into SECTIONS
// "Temporada N (ORDEM)" — NON-navigable headers (ADR-0021: only the episode is an
// entity), with the episode cards numbered in the chosen order. The order lives in the
// search param of the route; this module only consumes/fires the switch.

// rotuloOrdem gives the friendly name of an order: it maps the known ones (through the
// catalog) and prettifies the others (a new order from the source — e.g. a TMDB episode
// group — appears readable without touching here). Internal to the module (react-refresh: the component file only
// exports components) — exercised by the labels of the options of SeletorOrdenacao.
function rotuloOrdem(ordem: string, t: Translate): string {
  switch (ordem) {
    case 'official':
      return t('media.ordering.official')
    case 'dvd':
      return t('media.ordering.dvd')
    case 'absolute':
      return t('media.ordering.absolute')
    default:
      // An order that comes from a source with a name of its own (a TMDB episode
      // group key such as "blu-ray-order") reads better without the hyphens: the
      // key is an identifier, not a label.
      return ordem
        .split('-')
        .filter(Boolean)
        .map((parte) => parte.charAt(0).toUpperCase() + parte.slice(1))
        .join(' ')
  }
}

// SeletorOrdenacao is the dropdown of the available orders. The caller only renders it
// when there is more than one order (otherwise there is no choice). `onChange` receives the raw order
// (the caller decides to navigate/remove the param).
export function SeletorOrdenacao({
  value,
  options,
  onChange,
}: {
  value: string
  options: string[]
  onChange: (ordem: string) => void
}) {
  const t = useT()
  return (
    <Select
      aria-label={t('media.ordering.selectLabel')}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className="w-auto"
    >
      {options.map((o) => (
        <option key={o} value={o}>
          {rotuloOrdem(o, t)}
        </option>
      ))}
    </Select>
  )
}

// EpisodiosAgrupados renders the ALTERNATIVE order view: it fetches
// /entities/{id}/episodes?ordering=X and draws SECTIONS per group — each with a
// non-navigable header (`label`: "Temporada N", "Episódios" in the absolute order, or "Sem
// numeração") and the grid of navigable episodes (it reuses `GradeMembros`/`CardNumerado`, with the
// `numbering` already projected in order X by the backend). The `seletor` is passed
// by the caller and lives next to the title. Loading/error/empty states.
export function EpisodiosAgrupados({
  id,
  ordering,
  seletor,
}: {
  id: string
  ordering: string
  seletor: ReactNode
}) {
  const { lang } = useLanguage()
  const t = useT()
  const { data, isPending, isError } = useQuery(
    entityEpisodesQueryOptions(id, ordering, lang),
  )
  return (
    <section className="flex flex-col gap-4">
      <div className="flex items-center justify-between gap-3">
        <h2 className="font-heading text-lg font-medium">
          {t('common.entityTypePlural.episode')}
        </h2>
        {seletor}
      </div>
      {isPending ? <LoadingState label={t('media.ordering.loading')} /> : null}
      {isError ? <ErrorState title={t('media.ordering.loadError')} /> : null}
      {data && data.groups.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          {t('media.ordering.empty')}
        </p>
      ) : null}
      {data?.groups.map((grupo, indice) => (
        <div key={`${grupo.season}-${indice}`} className="flex flex-col gap-3">
          <h3 className="text-sm font-medium text-muted-foreground">
            {grupo.label}
          </h3>
          <GradeMembros
            itens={grupo.episodes}
            isPending={false}
            isError={false}
            carregando=""
            erro=""
            vazio={t('media.ordering.groupEmpty')}
            render={(card) => <CardNumerado card={card} />}
          />
        </div>
      ))}
    </section>
  )
}
