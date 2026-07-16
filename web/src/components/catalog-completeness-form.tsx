import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ErrorState,
  FormSaveFooter,
  LoadingState,
} from '@/components/query-states'
import { catalogCompletenessSettingsQueryOptions } from '@/lib/queries'
import {
  putCatalogCompletenessSettings,
  type CatalogCompletenessSettings,
  type TypeCompletenessConfig,
} from '@/lib/api'

// Formulário de admin da completude de CATALOGAÇÃO (briefing 0057): por tipo
// (`movie`/`tv`), quais campos/artes o dono considera desejáveis para um item
// estar bem catalogado. Conceito NOVO, DIFERENTE de completude de POSSE
// (`web/src/lib/entity-badges.ts`, 0036/0037) — este arquivo não reusa nada de
// lá (nem `EstadoCompletude`/`temCompletude`/`unidadeCompletude`).
//
// Só `movie`/`tv` têm UI nesta rodada (ver §4/§7 do briefing) — os rótulos são
// fixos em pt-BR, sem descoberta dinâmica de tipos.
const TIPOS_UI: Array<{ tipo: 'movie' | 'tv'; rotulo: string }> = [
  { tipo: 'movie', rotulo: 'Filme' },
  { tipo: 'tv', rotulo: 'Série' },
]

// Kinds de arte ofertados nesta tela (briefing §6): `still`/`characterart`
// ficam fora de propósito (still é de episódio; characterart não foi pedido).
const KINDS_ARTE_UI: Array<{ kind: string; rotulo: string }> = [
  { kind: 'poster', rotulo: 'Poster' },
  { kind: 'fanart', rotulo: 'Fanart' },
  { kind: 'logo', rotulo: 'Logo' },
  { kind: 'clearart', rotulo: 'Clearart' },
  { kind: 'banner', rotulo: 'Banner' },
  { kind: 'thumb', rotulo: 'Thumb' },
  { kind: 'disc', rotulo: 'Capa de disco' },
]

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
  const { data, isPending, isError, error } = useQuery(
    catalogCompletenessSettingsQueryOptions,
  )

  return (
    <div>
      {isPending ? (
        <LoadingState label="Carregando completude de catalogação…" />
      ) : isError ? (
        <ErrorState
          title="Não foi possível carregar a completude de catalogação"
          message={error instanceof Error ? error.message : undefined}
        />
      ) : (
        <CompletenessSettingsForm inicial={data} />
      )}
    </div>
  )
}

// Formulário semeado pela config vigente. Recebe `inicial` já resolvido para
// semear o estado uma única vez (sem useEffect de sincronização) — mesmo
// padrão de `LanguageSettingsForm` em settings.tsx.
function CompletenessSettingsForm({
  inicial,
}: {
  inicial: CatalogCompletenessSettings
}) {
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
      {TIPOS_UI.map(({ tipo, rotulo }) => {
        const cfg = config[tipo] ?? CONFIG_VAZIA
        return (
          <div key={tipo} className="flex flex-col gap-3">
            <h2 className="text-sm font-semibold">{rotulo}</h2>

            <fieldset className="flex flex-col gap-2">
              <legend className="text-sm font-medium">Informações</legend>
              <div className="flex flex-col gap-1.5">
                <CampoCheckbox
                  label="Sinopse"
                  checked={cfg.description}
                  onChange={() => alternarCampo(tipo, 'description')}
                />
                <CampoCheckbox
                  label="Duração"
                  checked={cfg.runtime}
                  onChange={() => alternarCampo(tipo, 'runtime')}
                />
                <CampoCheckbox
                  label="Classificação indicativa"
                  checked={cfg.certification}
                  onChange={() => alternarCampo(tipo, 'certification')}
                />
                <CampoCheckbox
                  label="Palavras-chave"
                  checked={cfg.keywords}
                  onChange={() => alternarCampo(tipo, 'keywords')}
                />
              </div>
            </fieldset>

            <fieldset className="flex flex-col gap-2">
              <legend className="text-sm font-medium">Elenco + arquivo</legend>
              <div className="flex flex-col gap-1.5">
                <CampoCheckbox
                  label="Elenco"
                  checked={cfg.cast}
                  onChange={() => alternarCampo(tipo, 'cast')}
                />
                <CampoCheckbox
                  label="Arquivo de mídia"
                  checked={cfg.media_file}
                  onChange={() => alternarCampo(tipo, 'media_file')}
                />
              </div>
            </fieldset>

            <fieldset className="flex flex-col gap-2">
              <legend className="text-sm font-medium">Artes</legend>
              <div className="flex flex-col gap-1.5">
                {KINDS_ARTE_UI.map(({ kind, rotulo: rotuloKind }) => (
                  <CampoCheckbox
                    key={kind}
                    label={rotuloKind}
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
