import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from '@tanstack/react-router'
import { ArrowLeft, Pencil } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { entityQueryOptions, overrideBlobQueryOptions } from '@/lib/queries'
import { ehDraft, rotuloTipo } from '@/lib/entity-badges'
import { useLanguage } from '@/lib/language-context'
import { rotuloIdioma } from '@/lib/languages'
import { temOverrideEditado } from '@/lib/override-fields'
import { MediaInfoTable } from '@/components/media-info-table'
import { OverrideEditor } from '@/components/override-editor'
import { ErrorState, LoadingState } from '@/components/query-states'

// Idioma efetivo de um campo traduzível, se caiu no fallback. Lê apenas o mapa
// `resolved_languages` (proveniência, ADR-0019): devolve o código do idioma que
// serviu o valor quando ele existe, não é a base neutra (`""`) e difere do
// idioma pedido — senão `null` (sem selo). Nenhuma lógica de idioma na UI além
// da comparação com o `lang` pedido.
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

// Selo "editado": há override do dono NO IDIOMA ATUAL para o campo (lido do blob
// cru). Convive com o selo "em {idioma}" de fallback — herdado × editado são
// estados distintos.
function SeloEditado({ ativo }: { ativo: boolean }) {
  return ativo ? <Badge variant="default">editado</Badge> : null
}

// Rota `/entities/$id`: cabeçalho (título/tipo/status) + lista de arquivos com
// mediainfo legível. Itens não-identificados aparecem como `draft`. O idioma de
// leitura vem do LanguageProvider e vai ao `?lang=` (re-busca ao trocar). O botão
// "Editar" abre o editor de override POR IDIOMA (briefing 0016).
export function EntityDetailScreen() {
  const { id } = useParams({ from: '/entities/$id' })
  const { lang } = useLanguage()
  const { data, isPending, isError, error } = useQuery(
    entityQueryOptions(id, lang),
  )
  // Blob CRU do override: alimenta os selos "editado" e semeia o editor. Sob o
  // prefixo `['entity', id]` — invalidado junto ao salvar/limpar.
  const overrideQuery = useQuery(overrideBlobQueryOptions(id))
  const blob = overrideQuery.data
  const [editando, setEditando] = useState(false)

  return (
    <main className="mx-auto max-w-4xl px-4 py-6">
      <Link
        to="/"
        className="mb-4 inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
      >
        <ArrowLeft className="size-4" aria-hidden />
        Voltar à biblioteca
      </Link>

      {isPending ? (
        <LoadingState label="Carregando item…" />
      ) : isError ? (
        <ErrorState
          title="Não foi possível carregar o item"
          message={error instanceof Error ? error.message : undefined}
        />
      ) : (
        <>
          <header className="mb-6 flex flex-col gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="font-heading text-2xl font-semibold">
                {data.title}
              </h1>
              <Badge variant="outline">{rotuloTipo(data.type)}</Badge>
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
            </div>
            {data.description ? (
              <div className="flex flex-col gap-1">
                <p className="text-sm text-muted-foreground">
                  {data.description}
                </p>
                <div className="flex flex-wrap items-center gap-1.5">
                  {(() => {
                    const efetivo = idiomaFallback(
                      data.resolved_languages,
                      'description',
                      lang,
                    )
                    return efetivo ? (
                      <Badge variant="secondary" className="w-fit">
                        em {rotuloIdioma(efetivo)}
                      </Badge>
                    ) : null
                  })()}
                  <SeloEditado
                    ativo={temOverrideEditado(blob, 'description', lang, true)}
                  />
                </div>
              </div>
            ) : null}

            {data.tags.length > 0 ? (
              <div className="flex flex-wrap items-center gap-1.5">
                {data.tags.map((tag) => (
                  <Badge key={tag} variant="outline">
                    {tag}
                  </Badge>
                ))}
                {(() => {
                  const efetivo = idiomaFallback(
                    data.resolved_languages,
                    'tags',
                    lang,
                  )
                  return efetivo ? (
                    <Badge variant="secondary">
                      em {rotuloIdioma(efetivo)}
                    </Badge>
                  ) : null
                })()}
                <SeloEditado
                  ativo={temOverrideEditado(blob, 'tags', lang, true)}
                />
              </div>
            ) : null}
          </header>

          {editando && blob !== undefined ? (
            <OverrideEditor
              key={`${lang}:${overrideQuery.dataUpdatedAt}`}
              entity={data}
              blob={blob}
              lang={lang}
              onDone={() => setEditando(false)}
            />
          ) : null}

          <section className="mt-6 flex flex-col gap-3">
            <h2 className="font-heading text-lg font-medium">Arquivos</h2>
            <MediaInfoTable entityId={id} />
          </section>
        </>
      )}
    </main>
  )
}
