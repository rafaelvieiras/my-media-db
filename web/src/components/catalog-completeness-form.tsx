import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ErrorState,
  FormSaveFooter,
  LoadingState,
} from '@/components/query-states'
import { catalogCompletenessSettingsQueryOptions } from '@/lib/queries'
import { useT } from '@/lib/i18n'
import {
  putCatalogCompletenessSettings,
  type CatalogCompletenessSettings,
  type TypeCompletenessConfig,
} from '@/lib/api'

// Admin form of the CATALOGING completeness: per type
// (`movie`/`tv`), which fields/artwork the owner considers desirable for an item
// to be well cataloged. A NEW concept, DIFFERENT from OWNERSHIP completeness
// (`web/src/lib/entity-badges.ts`) — this file reuses nothing from
// there (nor `EstadoCompletude`/`temCompletude`/`unidadeCompletude`).
//
// Only `movie`/`tv` have a UI in this round (see §4/§7) — the labels come from the
// catalog (`review.completeness.*`), without dynamic type discovery.
const TIPOS_UI: Array<'movie' | 'tv'> = ['movie', 'tv']

// Artwork kinds offered on this screen (§6): `still`/`characterart`
// stay out on purpose (still is for episodes; characterart was not requested).
const KINDS_ARTE_UI = [
  'poster',
  'fanart',
  'logo',
  'clearart',
  'banner',
  'thumb',
  'disc',
] as const

const CONFIG_VAZIA: TypeCompletenessConfig = {
  description: false,
  runtime: false,
  certification: false,
  keywords: false,
  cast: false,
  media_file: false,
  artwork: [],
}

export function CatalogCompletenessForm() {
  const t = useT()
  const { data, isPending, isError, error } = useQuery(
    catalogCompletenessSettingsQueryOptions,
  )

  return (
    <div>
      {isPending ? (
        <LoadingState label={t('review.completeness.loading')} />
      ) : isError ? (
        <ErrorState
          title={t('review.completeness.loadError')}
          message={error instanceof Error ? error.message : undefined}
        />
      ) : (
        <CompletenessSettingsForm inicial={data} />
      )}
    </div>
  )
}

// Form seeded with the current config. It receives `inicial` already resolved to
// seed the state a single time (no synchronization useEffect) — same
// pattern as `LanguageSettingsForm` in settings.tsx.
function CompletenessSettingsForm({
  inicial,
}: {
  inicial: CatalogCompletenessSettings
}) {
  const t = useT()
  const queryClient = useQueryClient()
  const [config, setConfig] = useState<CatalogCompletenessSettings>(() => ({
    ...inicial,
    movie: inicial.movie ?? CONFIG_VAZIA,
    tv: inicial.tv ?? CONFIG_VAZIA,
  }))

  const mutation = useMutation({
    mutationFn: (body: CatalogCompletenessSettings) =>
      putCatalogCompletenessSettings(body),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ['settings', 'catalog-completeness'],
      })
    },
  })

  function alternarCampo(
    tipo: string,
    campo: keyof Omit<TypeCompletenessConfig, 'artwork'>,
  ) {
    setConfig((prev) => {
      const atual = prev[tipo] ?? CONFIG_VAZIA
      return {
        ...prev,
        [tipo]: { ...atual, [campo]: !atual[campo] },
      }
    })
  }

  function alternarArtwork(tipo: string, kind: string) {
    setConfig((prev) => {
      const atual = prev[tipo] ?? CONFIG_VAZIA
      const artwork = atual.artwork.includes(kind)
        ? atual.artwork.filter((k) => k !== kind)
        : [...atual.artwork, kind]
      return { ...prev, [tipo]: { ...atual, artwork } }
    })
  }

  function onSubmit(e: React.FormEvent) {
    e.preventDefault()
    mutation.mutate(config)
  }

  const erroServidor =
    mutation.isError && mutation.error instanceof Error
      ? mutation.error.message
      : null

  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-6">
      {TIPOS_UI.map((tipo) => {
        const cfg = config[tipo] ?? CONFIG_VAZIA
        return (
          <div key={tipo} className="flex flex-col gap-3">
            <h2 className="text-sm font-semibold">
              {t(
                tipo === 'movie'
                  ? 'common.entityType.movie'
                  : 'common.entityType.tv',
              )}
            </h2>

            <fieldset className="flex flex-col gap-2">
              <legend className="text-sm font-medium">
                {t('review.completeness.information')}
              </legend>
              <div className="flex flex-col gap-1.5">
                <CampoCheckbox
                  label={t('review.completeness.field.description')}
                  checked={cfg.description}
                  onChange={() => alternarCampo(tipo, 'description')}
                />
                <CampoCheckbox
                  label={t('review.completeness.field.runtime')}
                  checked={cfg.runtime}
                  onChange={() => alternarCampo(tipo, 'runtime')}
                />
                <CampoCheckbox
                  label={t('review.completeness.field.certification')}
                  checked={cfg.certification}
                  onChange={() => alternarCampo(tipo, 'certification')}
                />
                <CampoCheckbox
                  label={t('review.completeness.field.keywords')}
                  checked={cfg.keywords}
                  onChange={() => alternarCampo(tipo, 'keywords')}
                />
              </div>
            </fieldset>

            <fieldset className="flex flex-col gap-2">
              <legend className="text-sm font-medium">
                {t('review.completeness.castAndFile')}
              </legend>
              <div className="flex flex-col gap-1.5">
                <CampoCheckbox
                  label={t('review.completeness.field.cast')}
                  checked={cfg.cast}
                  onChange={() => alternarCampo(tipo, 'cast')}
                />
                <CampoCheckbox
                  label={t('review.completeness.field.media_file')}
                  checked={cfg.media_file}
                  onChange={() => alternarCampo(tipo, 'media_file')}
                />
              </div>
            </fieldset>

            <fieldset className="flex flex-col gap-2">
              <legend className="text-sm font-medium">
                {t('review.completeness.artwork')}
              </legend>
              <div className="flex flex-col gap-1.5">
                {KINDS_ARTE_UI.map((kind) => (
                  <CampoCheckbox
                    key={kind}
                    label={t(`review.completeness.artworkKind.${kind}`)}
                    checked={cfg.artwork.includes(kind)}
                    onChange={() => alternarArtwork(tipo, kind)}
                  />
                ))}
              </div>
            </fieldset>
          </div>
        )
      })}

      <FormSaveFooter
        erro={erroServidor}
        sucesso={mutation.isSuccess}
        salvando={mutation.isPending}
      />
    </form>
  )
}

function CampoCheckbox({
  label,
  checked,
  onChange,
}: {
  label: string
  checked: boolean
  onChange: () => void
}) {
  return (
    <label className="flex items-center gap-2 text-sm">
      <input
        type="checkbox"
        className="size-4 accent-primary"
        checked={checked}
        onChange={onChange}
      />
      <span>{label}</span>
    </label>
  )
}
