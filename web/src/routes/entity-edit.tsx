import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams } from '@tanstack/react-router'
import { ArrowLeft } from 'lucide-react'
import { SchemaForm } from '@/components/schema-form'
import { ExternalIdsEditor } from '@/components/external-ids-editor'
import { ErrorState, LoadingState } from '@/components/query-states'
import {
  ApiError,
  clearOverrideField,
  patchOverride,
  type FieldDescriptor,
  type OverrideBlob,
  type OverridePatch,
  type ResolvedEntity,
} from '@/lib/api'
import {
  entityFieldsQueryOptions,
  entityQueryOptions,
  overrideBlobQueryOptions,
} from '@/lib/queries'
import { useLanguage } from '@/lib/language-context'
import {
  construirPatchEdicao,
  ordenados,
  schemaZod,
  valoresDeEdicao,
  type ValoresForm,
} from '@/lib/field-schema'

// Rota `/entities/$id/edit` (briefing 0030): a edição do override, antes inline no
// detalhe, agora em página própria (deep-link + testável). O formulário é DIRIGIDO
// PELOS DESCRITORES e preserva a semântica por-idioma do editor antigo (0016):
//   - campos TRADUZÍVEIS editam o idioma de leitura corrente; NEUTROS valem p/ todos;
//   - input esvaziado ao Salvar manda `null` (DEFINIR VAZIO); "Limpar" é DELETE por
//     idioma/da chave (volta a HERDAR) — ações distintas;
//   - tags como {add,remove} relativas ao resolvido ao abrir.
// A identidade externa (`external_ids`) segue como editor à parte (com seu próprio
// salvar/re-enriquecer). Sucesso do override volta para `/entities/$id`; Cancelar
// volta sem gravar; id inexistente → estado 404.
export function EntityEditScreen() {
  const { id } = useParams({ from: '/entities/$id/edit' })
  const { lang } = useLanguage()

  const entityQuery = useQuery(entityQueryOptions(id, lang))
  const blobQuery = useQuery(overrideBlobQueryOptions(id))
  const fieldsQuery = useQuery(entityFieldsQueryOptions)

  const voltar = (
    <Link
      to="/entities/$id"
      params={{ id }}
      className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
    >
      <ArrowLeft className="size-4" aria-hidden />
      Voltar ao item
    </Link>
  )

  const carregando =
    entityQuery.isPending || blobQuery.isPending || fieldsQuery.isPending
  const comErro =
    entityQuery.isError || blobQuery.isError || fieldsQuery.isError

  return (
    <main className="mx-auto max-w-2xl px-4 py-6">
      {voltar}
      {carregando ? (
        <LoadingState label="Carregando item…" />
      ) : comErro ? (
        <ErrorState
          title="Não foi possível carregar o item"
          message={
            entityQuery.error instanceof Error
              ? entityQuery.error.message
              : undefined
          }
        />
      ) : (
        <EntityEditForm
          id={id}
          lang={lang}
          entity={entityQuery.data}
          blob={blobQuery.data}
          fields={fieldsQuery.data}
          // Remonta o form quando o idioma muda ou o blob revalida (após "Limpar"),
          // recompondo as sementes — o mesmo padrão do editor antigo.
          key={`${lang}:${blobQuery.dataUpdatedAt}`}
        />
      )}
    </main>
  )
}

function EntityEditForm({
  id,
  lang,
  entity,
  blob,
  fields,
}: {
  id: string
  lang: string
  entity: ResolvedEntity
  blob: OverrideBlob
  fields: FieldDescriptor[]
}) {
  const qc = useQueryClient()
  const navigate = useNavigate()

  const sementes = valoresDeEdicao(fields, blob, lang, entity)

  // Invalidação pós-edição: `['entity', id]` é PREFIXO — derruba a entity em todas
  // as variações de `lang`, os arquivos e o blob de override; `['entities']`
  // atualiza os cards da lista.
  function invalidar() {
    qc.invalidateQueries({ queryKey: ['entity', id] })
    qc.invalidateQueries({ queryKey: ['entities'] })
  }

  const salvar = useMutation({
    mutationFn: (patch: OverridePatch) => patchOverride(id, lang, patch),
    onSuccess: () => {
      invalidar()
      navigate({ to: '/entities/$id', params: { id } })
    },
  })

  // "Limpar": com `lang` para traduzível (volta a herdar naquele idioma), sem
  // `lang` para neutro (remove a chave). Não navega — a revalidação do blob
  // remonta o form (via `key`) com o valor herdado.
  const limpar = useMutation({
    mutationFn: ({
      campo,
      traduzivel,
    }: {
      campo: string
      traduzivel: boolean
    }) => clearOverrideField(id, campo, traduzivel ? lang : undefined),
    onSuccess: invalidar,
  })

  const ocupado = salvar.isPending || limpar.isPending

  function onSubmit(valores: ValoresForm) {
    const patch = construirPatchEdicao(fields, valores, sementes)
    if (Object.keys(patch).length === 0) {
      // Nada mudou — volta ao detalhe sem bater na API.
      navigate({ to: '/entities/$id', params: { id } })
      return
    }
    salvar.mutate(patch)
  }

  const mensagemErro =
    salvar.error instanceof ApiError
      ? salvar.error.message
      : limpar.error instanceof ApiError
        ? limpar.error.message
        : salvar.isError || limpar.isError
          ? 'Não foi possível salvar as alterações.'
          : null

  return (
    <div className="mt-4 flex flex-col gap-6">
      <SchemaForm
        fields={ordenados(fields)}
        defaultValues={sementes}
        schema={schemaZod(fields, 'edicao')}
        onSubmit={onSubmit}
        onCancel={() => navigate({ to: '/entities/$id', params: { id } })}
        submitLabel="Salvar alterações"
        submitPendingLabel="Salvando…"
        isPending={ocupado}
        mensagemErro={mensagemErro}
        placeholders={placeholdersDe(fields, entity)}
        onClear={(campo, traduzivel) => limpar.mutate({ campo, traduzivel })}
        clearDisabled={ocupado}
        lang={lang}
      />

      {/* Identidade externa (0027): fixar os ids da fonte é EDIÇÃO avançada, então
          vive na rota de edição com seu próprio salvar/re-enriquecer. Não é
          override — grava a identidade autoritativa (MD4) e re-resolve. */}
      <ExternalIdsEditor id={id} externalIds={entity.external_ids} />
    </div>
  )
}

// placeholdersDe monta o texto herdado/resolvido por campo (mostrado quando o
// input está vazio), iterando os descritores: traduzíveis/neutros usam o valor
// resolvido; a nota local mostra a nota da FONTE (distinta da sua). Campos sem
// valor resolvido caem no default do próprio descritor.
function placeholdersDe(
  fields: FieldDescriptor[],
  entity: ResolvedEntity,
): Record<string, string> {
  const out: Record<string, string> = {}
  for (const d of fields) {
    if (d.name === 'local_rating') {
      out[d.name] =
        entity.vote_average != null
          ? `Fonte: ${entity.vote_average.toFixed(1)}`
          : 'Sem nota'
      continue
    }
    const valor = (entity as unknown as Record<string, unknown>)[d.name]
    if (typeof valor === 'string' && valor !== '') {
      out[d.name] = valor
    } else if (d.widget === 'date') {
      out[d.name] = 'AAAA-MM-DD'
    }
  }
  return out
}
