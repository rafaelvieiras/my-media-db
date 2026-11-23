import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { EyeOff } from 'lucide-react'
import { CardNumerado, GradeMembros } from '@/components/entity-members'
import { Switch } from '@/components/ui/switch'
import { useT } from '@/lib/i18n'
import { useLanguage } from '@/lib/language-context'
import { entityMembersQueryOptions } from '@/lib/queries'
import type { EntityCard } from '@/lib/api'

// ListaEpisodiosTemporada is the episode list of ONE season with the library cut in two:
// the episodes the owner HAS, and the ones the provider publishes and the folder does not.
//
// The second group is HIDDEN by default and carries no highlight when it appears -- it is a
// gap in the collection, not a task. The owner opens it when he wants to check the shape of
// what he has: an episode 13 in a 12-episode season, or a file that did not land on the right
// episode. Before this, the whole season was one list and the missing episodes sat between
// the owned ones with the same weight.
export function ListaEpisodiosTemporada({ id }: { id: string }) {
  const t = useT()
  const { lang } = useLanguage()
  const [mostrarFora, setMostrarFora] = useState(false)
  const { data, isPending, isError } = useQuery(
    entityMembersQueryOptions(id, lang),
  )

  const naBiblioteca = (data ?? []).filter((card) => card.owned !== false)
  const foraDaBiblioteca = (data ?? []).filter((card) => card.owned === false)

  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-medium">
        {t('entity.detail.episodes')}
      </h2>
      <GradeMembros
        itens={naBiblioteca}
        isPending={isPending}
        isError={isError}
        carregando={t('entity.detail.episodesLoading')}
        erro={t('entity.detail.episodesError')}
        vazio={t('entity.detail.episodesEmpty')}
        render={(card) => <CardNumerado card={card} />}
      />

      {foraDaBiblioteca.length > 0 ? (
        <div className="flex flex-col gap-3">
          <label className="flex items-center gap-2 text-sm text-muted-foreground">
            <Switch
              checked={mostrarFora}
              onCheckedChange={setMostrarFora}
              aria-label={t('entity.detail.showMissingEpisodes')}
            />
            {t('entity.detail.showMissingEpisodesCount', {
              n: foraDaBiblioteca.length,
            })}
          </label>
          {mostrarFora ? (
            <ul
              className="grid grid-cols-2 gap-4 opacity-60 sm:grid-cols-3 lg:grid-cols-4"
              data-testid="episodios-fora-da-biblioteca"
            >
              {foraDaBiblioteca.map((card) => (
                <li key={card.id} className="flex flex-col gap-1">
                  <CardNumerado card={card} />
                  <span className="flex items-center gap-1 text-xs text-muted-foreground">
                    <EyeOff className="size-3" aria-hidden />
                    {t('entity.detail.notInLibrary')}
                  </span>
                </li>
              ))}
            </ul>
          ) : null}
        </div>
      ) : null}
    </section>
  )
}

// reexporta o tipo para os testes que montam cards de episódio
export type { EntityCard }
