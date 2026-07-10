import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from '@tanstack/react-router'
import { ArrowLeft, Pencil } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { entityQueryOptions, overrideBlobQueryOptions } from '@/lib/queries'
import { ehDraft } from '@/lib/entity-badges'
import { useLanguage } from '@/lib/language-context'
import { rotuloIdioma } from '@/lib/languages'
import { temOverrideEditado } from '@/lib/override-fields'
import { ArtGallery } from '@/components/art-gallery'
import { CollectionMembers } from '@/components/collection-members'
import { MediaInfoTable } from '@/components/media-info-table'
import { OverrideEditor } from '@/components/override-editor'
import { ErrorState, LoadingState } from '@/components/query-states'
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

export function EntityDetailScreen() {
  const { id } = useParams({ from: '/entities/$id' })
  const { lang } = useLanguage()
  const { data, isPending, isError, error } = useQuery(
    entityQueryOptions(id, lang),
  )
  // Blob CRU do override: alimenta os selos "editado" e semeia o editor.
  const overrideQuery = useQuery(overrideBlobQueryOptions(id))
  const blob = overrideQuery.data
  const [editando, setEditando] = useState(false)

  const voltar = (
    <Link
      to="/"
      className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
    >
      <ArrowLeft className="size-4" aria-hidden />
      Voltar à biblioteca
    </Link>
  )

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

  return (
    <main className="mx-auto max-w-4xl px-4 py-6">
      <RichDetailView
        detail={deResolvedEntity(data)}
        source="tmdb"
        back={voltar}
        titleBadges={
          <>
            {ehDraft(data.status) ? (
              <Badge variant="secondary">não identificado</Badge>
            ) : (
              <Badge variant="outline">{data.status}</Badge>
            )}
            <SeloEditado
              ativo={temOverrideEditado(blob, 'title', lang, true)}
            />
            {!editando ? (
              <Button
                variant="outline"
                size="sm"
                className="ml-auto"
                onClick={() => setEditando(true)}
              >
                <Pencil aria-hidden />
                Editar
              </Button>
            ) : null}
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
              ativo={temOverrideEditado(blob, 'description', lang, true)}
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
            <SeloEditado ativo={temOverrideEditado(blob, 'tags', lang, true)} />
          </>
        }
      >
        {editando && blob !== undefined ? (
          <OverrideEditor
            key={`${lang}:${overrideQuery.dataUpdatedAt}`}
            entity={data}
            blob={blob}
            lang={lang}
            onDone={() => setEditando(false)}
          />
        ) : null}

        {/* Coleção (0025): mostra os MEMBROS no lugar dos Arquivos (coleção não tem
            media_file). A galeria de arte segue valendo (arte própria da coleção). */}
        {data.type === 'collection' ? <CollectionMembers id={id} /> : null}

        <ArtGallery entityId={id} />

        {data.type !== 'collection' ? (
          <section className="flex flex-col gap-3">
            <h2 className="font-heading text-lg font-medium">Arquivos</h2>
            <MediaInfoTable entityId={id} />
          </section>
        ) : null}
      </RichDetailView>
    </main>
  )
}
