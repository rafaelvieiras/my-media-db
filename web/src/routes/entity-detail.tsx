import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams, useSearch } from '@tanstack/react-router'
import { Trash2, TriangleAlert } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ApiError, purgeEntity, type OrderingsInfo } from '@/lib/api'
import {
  artworkQueryOptions,
  entityFieldsQueryOptions,
  entityQueryOptions,
  overrideBlobQueryOptions,
} from '@/lib/queries'
import { ehDraft, rotuloStatus } from '@/lib/entity-badges'
import {
  precisaAtencaoCatalogacao,
  rotuloItemFaltando,
  type CatalogCompleteness,
} from '@/lib/catalog-completeness-badges'
import { useLanguage } from '@/lib/language-context'
import { rotuloIdioma } from '@/lib/languages'
import { temOverrideEditado } from '@/lib/override-fields'
import { ArtGallery } from '@/components/art-gallery'
import { CollectionMembers } from '@/components/collection-members'
import { EntityActionButtons } from '@/components/entity-action-buttons'
import { MembrosSomenteLeitura } from '@/components/entity-members'
import {
  EpisodiosAgrupados,
  SeletorOrdenacao,
} from '@/components/episode-ordering'
import { MediaInfoTable } from '@/components/media-info-table'
import {
  ErrorState,
  LoadingState,
  VoltarBiblioteca,
} from '@/components/query-states'
import { Migalhas } from '@/components/migalhas'
import { RichDetailView } from '@/components/rich-detail-view'
import { deResolvedEntity } from '@/lib/rich-detail'

// Rota `/entities/$id`: o detalhe da entity IMPORTADA. Usa a MESMA RichDetailView do
// detalhe do externo (hero, slogan, metadados, gêneros, keywords, vídeos, coleção,
// similares, links) — o backend serve os campos ricos no ResolvedEntity (paridade
// 0022/0023). O específico da entity entra por SLOTS: os selos de override (herdado ×
// editado × idioma de fallback), o botão/editor de override (0016), a galeria de arte
// (0017) e a tabela de arquivos com mediainfo. O idioma de leitura vem do
// LanguageProvider e vai ao `?lang=` (re-busca ao trocar).

// idiomaFallback devolve o idioma efetivo de um campo traduzível quando ele caiu no
// fallback (lê só `resolved_languages`, ADR-0019): o código que serviu o valor quando
// existe, não é a base neutra (`""`) e difere do pedido — senão null (sem selo).
function idiomaFallback(
  resolved: Record<string, string>,
  campo: string,
  lang: string,
): string | null {
  const efetivo = resolved[campo]
  if (efetivo && efetivo !== '' && efetivo !== lang) {
    return efetivo
  }
  return null
}

// SeloFallback mostra "em {idioma}" quando o campo caiu no fallback de idioma.
function SeloFallback({
  resolved,
  campo,
  lang,
}: {
  resolved: Record<string, string>
  campo: string
  lang: string
}) {
  const efetivo = idiomaFallback(resolved, campo, lang)
  return efetivo ? (
    <Badge variant="secondary" className="w-fit">
      em {rotuloIdioma(efetivo)}
    </Badge>
  ) : null
}

// SeloEditado marca override do dono NO IDIOMA ATUAL para o campo (lido do blob cru).
function SeloEditado({ ativo }: { ativo: boolean }) {
  return ativo ? <Badge variant="default">editado</Badge> : null
}

// SeloPosse mostra a posse AGREGADA de uma temporada/série (0034): "possui 12/13
// episódios". Discreto (secondary). Null quando não há agregação (a API só envia
// `ownership` para tv/season; movie/episódio/coleção usam a posse simples).
function SeloPosse({
  ownership,
}: {
  ownership: { owned_count: number; total_count: number } | null
}) {
  if (!ownership) {
    return null
  }
  return (
    <Badge variant="secondary" className="w-fit">
      possui {ownership.owned_count}/{ownership.total_count} episódios
    </Badge>
  )
}

// ItemFaltandoComAcao renderiza UM item de `missing`. `media_file` (arquivo de
// mídia) é o único que o botão genérico "Baixar informações faltando"
// (re-enriquecimento via provider) comprovadamente NÃO resolve — só se resolve
// vinculando um datasource/rodando um scan em Bibliotecas, tela diferente —
// então ganha um link junto ao rótulo (briefing 0077). As demais chaves (campo/
// arte) continuam só texto, pois o botão genérico já resolve.
function ItemFaltandoComAcao({ chave }: { chave: string }) {
  if (chave === 'media_file') {
    return (
      <li>
        <span>{rotuloItemFaltando(chave)}</span>
        {' — '}
        <Link
          to="/library"
          className="underline underline-offset-2 hover:text-foreground"
        >
          ver Bibliotecas
        </Link>
      </li>
    )
  }
  return <li>{rotuloItemFaltando(chave)}</li>
}

// SecaoCompletudeIncompleta lista, PT-BR, o que falta preencher para o item
// atender a régua de completude de CATALOGAÇÃO (0057/0058) — DIFERENTE da posse
// (`SeloPosse` acima, 0034/0036). Fica perto do topo/metadados (não escondida no
// fim da página), sempre visível (sem depender de hover — útil em touch/mobile,
// diferente do tooltip do card). Só renderiza quando incompleto; item que atende a
// régua (ou tipo sem régua, ou draft) não mostra nada — sem confirmação positiva,
// mesmo princípio de "sinalizar só o que precisa de atenção" do 0038.
function SecaoCompletudeIncompleta({ cc }: { cc: CatalogCompleteness }) {
  if (!precisaAtencaoCatalogacao(cc)) {
    return null
  }
  return (
    <section
      aria-label="Catalogação incompleta"
      className="flex flex-col gap-2 rounded-xl bg-amber-500/10 p-4 ring-1 ring-amber-500/30"
    >
      <h2 className="flex items-center gap-1.5 text-sm font-semibold text-amber-600 dark:text-amber-400">
        <TriangleAlert className="size-4" aria-hidden />
        Catalogação incompleta
      </h2>
      <ul className="list-inside list-disc text-sm text-muted-foreground">
        {cc?.missing.map((chave) => (
          <ItemFaltandoComAcao key={chave} chave={chave} />
        ))}
      </ul>
    </section>
  )
}

// ExcluirColecao apaga a entity COLEÇÃO (briefing 0026) via DELETE /entities/{id}
// (purga com CASCADE das relações — os filmes membros NÃO são apagados). Ação
// DESTRUTIVA, então confirma em DUAS etapas (mesmo padrão da remoção de membro) e
// fica discreta. No sucesso, invalida a lista e volta para a biblioteca.
function ExcluirColecao({ id }: { id: string }) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [confirmando, setConfirmando] = useState(false)
  const mutation = useMutation({
    mutationFn: () => purgeEntity(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['entities'] })
      navigate({ to: '/' })
    },
  })
  const mensagemErro =
    mutation.error instanceof ApiError ? mutation.error.message : null

  return (
    <div className="flex flex-wrap items-center justify-end gap-2">
      {mensagemErro ? (
        <span role="alert" className="text-sm font-medium text-destructive">
          {mensagemErro}
        </span>
      ) : null}
      {confirmando ? (
        <>
          <Button
            variant="destructive"
            size="sm"
            disabled={mutation.isPending}
            onClick={() => mutation.mutate()}
          >
            {mutation.isPending ? 'Excluindo…' : 'Excluir coleção?'}
          </Button>
          <Button
            variant="ghost"
            size="sm"
            disabled={mutation.isPending}
            onClick={() => setConfirmando(false)}
          >
            Cancelar
          </Button>
        </>
      ) : (
        <Button
          variant="ghost"
          size="sm"
          className="text-destructive"
          onClick={() => setConfirmando(true)}
        >
          <Trash2 aria-hidden />
          Excluir coleção
        </Button>
      )}
    </div>
  )
}

// SecaoSerie decide a view da árvore de uma SÉRIE conforme a ordem escolhida (0040):
// o seletor aparece só quando há mais de uma ordem; a ordem default (ou ausente)
// mostra os cards de temporada navegáveis (a view de sempre); uma ordem alternativa
// mostra as seções de episódios reagrupados. Trocar de ordem navega no search param
// (voltar ao default REMOVE o param — URL limpa, sem persistência).
function SecaoSerie({
  id,
  orderings,
  ordering,
}: {
  id: string
  orderings: OrderingsInfo | null
  ordering: string | undefined
}) {
  const navigate = useNavigate()
  const def = orderings?.default
  const mostrarSeletor = (orderings?.available.length ?? 0) > 1
  // Ordem "alternativa" = escolhida e != default. Só ela dispara a view agrupada; o
  // default (ou ausente) cai na view de temporadas.
  const alternativa = ordering && ordering !== def ? ordering : null

  const trocar = (novo: string) => {
    navigate({
      to: '/entities/$id',
      params: { id },
      search: def && novo === def ? {} : { ordering: novo },
    })
  }

  const seletor =
    mostrarSeletor && def && orderings ? (
      <SeletorOrdenacao
        value={ordering ?? def}
        options={orderings.available}
        onChange={trocar}
      />
    ) : null

  if (alternativa) {
    return (
      <EpisodiosAgrupados id={id} ordering={alternativa} seletor={seletor} />
    )
  }
  return (
    <MembrosSomenteLeitura
      id={id}
      titulo="Temporadas"
      carregando="Carregando temporadas…"
      erro="Não foi possível carregar as temporadas"
      vazio="Esta série ainda não tem temporadas."
      acao={seletor}
    />
  )
}

export function EntityDetailScreen() {
  const { id } = useParams({ from: '/entities/$id' })
  const { ordering } = useSearch({ from: '/entities/$id' })
  const { lang } = useLanguage()
  const { data, isPending, isError, error } = useQuery(
    entityQueryOptions(id, lang),
  )
  // Blob CRU do override + descritores: alimentam os selos "editado" (o selo lê o
  // blob e descobre se o campo é traduzível pelos descritores, briefing 0030).
  const overrideQuery = useQuery(overrideBlobQueryOptions(id))
  const blob = overrideQuery.data
  const fields = useQuery(entityFieldsQueryOptions).data
  // Inventário de artes (0048/0049): alimenta a FAIXA de selos do card do topo. A
  // MESMA query é lida pela ArtGallery (child) — o TanStack dedupe numa só ida.
  const artwork = useQuery(artworkQueryOptions(id)).data

  const voltar = <VoltarBiblioteca />

  if (isPending) {
    return (
      <main className="mx-auto max-w-4xl px-4 py-6">
        {voltar}
        <LoadingState label="Carregando item…" />
      </main>
    )
  }
  if (isError) {
    return (
      <main className="mx-auto max-w-4xl px-4 py-6">
        {voltar}
        <ErrorState
          title="Não foi possível carregar o item"
          message={error instanceof Error ? error.message : undefined}
        />
      </main>
    )
  }

  // Topo da tela: o "Voltar à biblioteca" e, para os nós da árvore de séries
  // (temporada/episódio), o breadcrumb ascendente logo abaixo (0032). Obra de topo
  // tem `ancestors` vazio → `Migalhas` não renderiza e o topo fica só com o voltar.
  const topo = (
    <div className="flex flex-col gap-2">
      {voltar}
      <Migalhas
        ancestors={data.ancestors}
        atual={{ title: data.title, type: data.type }}
      />
      <SecaoCompletudeIncompleta cc={data.catalog_completeness} />
    </div>
  )

  return (
    <main className="mx-auto max-w-4xl px-4 py-6">
      <RichDetailView
        detail={deResolvedEntity(data)}
        source="tmdb"
        back={topo}
        availableArtwork={artwork?.available}
        titleBadges={
          <>
            {ehDraft(data.status) ? (
              <Badge variant="secondary">não identificado</Badge>
            ) : (
              <Badge variant="outline">{rotuloStatus(data.status)}</Badge>
            )}
            <SeloEditado
              ativo={temOverrideEditado(blob, 'title', lang, fields)}
            />
            <SeloPosse ownership={data.ownership} />
            <EntityActionButtons
              id={id}
              catalogCompleteness={data.catalog_completeness}
              owned={data.owned}
            />
          </>
        }
        descriptionExtra={
          <div className="flex flex-wrap items-center gap-1.5">
            <SeloFallback
              resolved={data.resolved_languages}
              campo="description"
              lang={lang}
            />
            <SeloEditado
              ativo={temOverrideEditado(blob, 'description', lang, fields)}
            />
          </div>
        }
        genresExtra={
          <>
            <SeloFallback
              resolved={data.resolved_languages}
              campo="tags"
              lang={lang}
            />
            <SeloEditado
              ativo={temOverrideEditado(blob, 'tags', lang, fields)}
            />
          </>
        }
      >
        {/* Coleção (0025): mostra os MEMBROS no lugar dos Arquivos (coleção não tem
            media_file). A galeria de arte segue valendo (arte própria da coleção). O
            "Excluir coleção" (0026) fica ao fim, discreto (ação destrutiva). */}
        {data.type === 'collection' ? (
          <>
            <CollectionMembers id={id} editable />
            <ExcluirColecao id={id} />
          </>
        ) : null}

        {/* Árvore de séries (0031/0040), somente leitura: a série lista as TEMPORADAS
            navegáveis na ordem default; um seletor (0040) troca para uma ordem
            alternativa, que reagrupa os EPISÓDIOS em seções. A temporada lista os
            EPISÓDIOS numerados (0039). Reusa o molde pai-com-filhos (/members) sem o
            add/remove — a Fase 1 não edita a árvore à mão. */}
        {data.type === 'tv' ? (
          <SecaoSerie
            id={id}
            orderings={data.orderings ?? null}
            ordering={ordering}
          />
        ) : null}
        {data.type === 'season' ? (
          <MembrosSomenteLeitura
            id={id}
            titulo="Episódios"
            carregando="Carregando episódios…"
            erro="Não foi possível carregar os episódios"
            vazio="Esta temporada ainda não tem episódios."
            numerar
          />
        ) : null}

        <ArtGallery
          entityId={id}
          type={data.type}
          stillUrl={data.still_url ?? null}
          owned={data.owned}
        />

        {/* Arquivos só nos nós-folha que têm media_file: filme e episódio. Coleção,
            série e temporada agrupam filhos (sem arquivo próprio) — sem a seção. */}
        {data.type === 'movie' || data.type === 'episode' ? (
          <section className="flex flex-col gap-3">
            <h2 className="font-heading text-lg font-medium">Arquivos</h2>
            <MediaInfoTable entityId={id} />
          </section>
        ) : null}
      </RichDetailView>
    </main>
  )
}
