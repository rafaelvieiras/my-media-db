import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useParams } from '@tanstack/react-router'
import { SchemaForm } from '@/components/schema-form'
import { EntityEditShell } from '@/components/entity-edit-shell'
import { EntityEditEstadoCarga } from '@/components/query-states'
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

// Rota `/entities/$id/edit` (briefing 0030; dividida em subrotas pelo 0080): a
// seção "Informações básicas" — o formulário DIRIGIDO POR DESCRITORES, default
// da edição de item (mantém a URL de sempre, deep-links não quebram). Preserva a
// semântica por-idioma do editor antigo (0016):
//   - campos TRADUZÍVEIS editam o idioma de leitura corrente; NEUTROS valem p/ todos;
//   - input esvaziado ao Salvar manda `null` (DEFINIR VAZIO); "Limpar" é DELETE por
//     idioma/da chave (volta a HERDAR) — ações distintas;
//   - tags como {add,remove} relativas ao resolvido ao abrir.
// A sidebar/cabeçalho compartilhados vivem no `EntityEditShell`; IDs externos e
// elenco/equipe agora são subrotas próprias (`entity-edit-ids.tsx`/
// `entity-edit-creditos.tsx`). Sucesso do override volta para `/entities/$id`;
// Cancelar volta sem gravar; id inexistente → estado 404.
export function EntityEditBasicoScreen() {
  const { id } = useParams({ from: '/entities/$id/edit' })
  const { lang } = useLanguage()

  const entityQuery = useQuery(entityQueryOptions(id, lang))
  const blobQuery = useQuery(overrideBlobQueryOptions(id))
  const fieldsQuery = useQuery(entityFieldsQueryOptions)

  // Guard de "sair sem salvar" (0073/0080): só o "sujo" deste bloco — a subrota
  // monta um único bloco, então não há mais 3 booleanos para combinar.
  const [sujo, setSujo] = useState(false)

  const carregando =
    entityQuery.isPending || blobQuery.isPending || fieldsQuery.isPending
  const comErro =
    entityQuery.isError || blobQuery.isError || fieldsQuery.isError

  if (carregando || comErro || !entityQuery.data) {
    return (
      <EntityEditEstadoCarga carregando={carregando} erro={entityQuery.error} />
    )
  }

  return (
    <EntityEditShell
      id={id}
      entity={entityQuery.data}
      secaoAtiva="basico"
      sujo={sujo}
    >
      <EntityEditBasicoForm
        id={id}
        lang={lang}
        entity={entityQuery.data}
        blob={blobQuery.data!}
        fields={fieldsQuery.data!}
        onDirtyChange={setSujo}
        // Remonta o form quando o idioma muda ou o blob revalida (após "Limpar"),
        // recompondo as sementes — o mesmo padrão do editor antigo.
        key={`${lang}:${blobQuery.dataUpdatedAt}`}
      />
    </EntityEditShell>
  )
}

function EntityEditBasicoForm({
  id,
  lang,
  entity,
  blob,
  fields,
  onDirtyChange,
}: {
  id: string
  lang: string
  entity: ResolvedEntity
  blob: OverrideBlob
  fields: FieldDescriptor[]
  onDirtyChange: (sujo: boolean) => void
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
      onDirtyChange={onDirtyChange}
    />
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
