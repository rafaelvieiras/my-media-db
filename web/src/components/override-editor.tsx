import { useMemo, useState, type FormEvent, type KeyboardEvent } from 'react'
import {
  useMutation,
  useQueryClient,
  type QueryClient,
} from '@tanstack/react-query'
import { Eraser, Plus, X } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'
import {
  ApiError,
  clearOverrideField,
  patchOverride,
  type OverrideBlob,
  type OverridePatch,
  type ResolvedEntity,
} from '@/lib/api'
import { rotuloIdioma } from '@/lib/languages'

// Editor de override POR IDIOMA (briefing 0016). Os campos TRADUZÍVEIS
// (title/description/tags) editam o idioma de leitura corrente (`lang`); os
// NEUTROS (original_title/release_date) valem para todos os idiomas.
//
// Semântica "LIMPAR vs. DEFINIR VAZIO" (R6 por idioma):
//   - Ao entrar em edição, um campo traduzível é SEMEADO com o override do idioma
//     atual SE existir (`blob[campo][lang]`); senão fica vazio e o placeholder
//     mostra o valor herdado/resolvido. Um input que o dono deixa vazio ao Salvar
//     manda `null` = DEFINIR VAZIO (apaga a base e interrompe o fallback naquele
//     idioma). Para VOLTAR A HERDAR, o dono usa o botão "Limpar" (DELETE por
//     idioma) — são ações distintas e é isso que a UI precisa separar.
//
// Detecção de "editado" POR IDIOMA no blob cru vive em `@/lib/override-fields`
// (compartilhada com o detalhe).

// Valor semente de um campo traduzível: o override do idioma atual se for string;
// senão vazio (o placeholder mostra o herdado).
function semearTraduzivel(
  blob: OverrideBlob,
  campo: string,
  lang: string,
): string {
  const valor = blob[campo]
  if (valor && typeof valor === 'object' && !Array.isArray(valor)) {
    const doIdioma = (valor as Record<string, unknown>)[lang]
    return typeof doIdioma === 'string' ? doIdioma : ''
  }
  return ''
}

// Valor semente de um campo neutro: o escalar guardado se for string; senão vazio.
function semearNeutro(blob: OverrideBlob, campo: string): string {
  const valor = blob[campo]
  return typeof valor === 'string' ? valor : ''
}

// Valor semente da NOTA LOCAL (campo neutro NUMÉRICO): o número guardado como texto
// para o input; senão vazio (o placeholder mostra a nota da fonte quando houver).
function semearNota(blob: OverrideBlob): string {
  const valor = blob['local_rating']
  return typeof valor === 'number' ? String(valor) : ''
}

// Invalidação pós-edição: `['entity', id]` é PREFIXO — derruba a entity em todas
// as variações de `lang`, os arquivos e o blob de override de uma vez; `['entities']`
// atualiza os cards da lista.
function invalidarAposEdicao(qc: QueryClient, id: string) {
  qc.invalidateQueries({ queryKey: ['entity', id] })
  qc.invalidateQueries({ queryKey: ['entities'] })
}

const CLASSE_TEXTAREA = cn(
  'flex min-h-20 w-full rounded-lg border border-border bg-background px-2.5 py-1.5 text-sm shadow-xs outline-none',
  'transition-[color,box-shadow] placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30',
)

export function OverrideEditor({
  entity,
  blob,
  lang,
  onDone,
}: {
  entity: ResolvedEntity
  blob: OverrideBlob
  lang: string
  onDone: () => void
}) {
  const qc = useQueryClient()
  const id = entity.id

  // Sementes estáveis por montagem — o editor é remontado (via `key`) quando o
  // idioma muda ou o blob é revalidado, então recompor as sementes é seguro.
  const sementes = useMemo(
    () => ({
      title: semearTraduzivel(blob, 'title', lang),
      description: semearTraduzivel(blob, 'description', lang),
      original_title: semearNeutro(blob, 'original_title'),
      release_date: semearNeutro(blob, 'release_date'),
      local_rating: semearNota(blob),
      tags: entity.tags,
    }),
    [blob, lang, entity.tags],
  )

  const [title, setTitle] = useState(sementes.title)
  const [description, setDescription] = useState(sementes.description)
  const [originalTitle, setOriginalTitle] = useState(sementes.original_title)
  const [releaseDate, setReleaseDate] = useState(sementes.release_date)
  const [localRating, setLocalRating] = useState(sementes.local_rating)
  const [tags, setTags] = useState<string[]>(sementes.tags)
  const [tagInput, setTagInput] = useState('')

  const salvar = useMutation({
    mutationFn: (patch: OverridePatch) => patchOverride(id, lang, patch),
    onSuccess: () => {
      invalidarAposEdicao(qc, id)
      onDone()
    },
  })

  // Limpar um campo: com `lang` para traduzível (volta a herdar naquele idioma),
  // sem `lang` para neutro (remove a chave inteira). Não sai do modo de edição —
  // a revalidação do blob remonta o editor com o valor herdado.
  const limpar = useMutation({
    mutationFn: ({ campo, comLang }: { campo: string; comLang: boolean }) =>
      clearOverrideField(id, campo, comLang ? lang : undefined),
    onSuccess: () => invalidarAposEdicao(qc, id),
  })

  function construirPatch(): OverridePatch {
    const patch: Record<string, unknown> = {}
    // Traduzíveis + neutros escalares: só entram se mudaram. Vazio = `null`
    // (DEFINIR VAZIO), não confundir com "Limpar".
    if (title !== sementes.title) {
      patch.title = title.trim() === '' ? null : title
    }
    if (description !== sementes.description) {
      patch.description = description.trim() === '' ? null : description
    }
    if (originalTitle !== sementes.original_title) {
      patch.original_title = originalTitle.trim() === '' ? null : originalTitle
    }
    if (releaseDate !== sementes.release_date) {
      patch.release_date = releaseDate.trim() === '' ? null : releaseDate
    }
    // Nota local (neutra, numérica): vazio → null (limpa); senão o número. O input
    // type=number restringe a entrada; o backend valida a faixa 0–10.
    if (localRating !== sementes.local_rating) {
      const t = localRating.trim()
      patch.local_rating = t === '' ? null : Number(t)
    }
    // tags como operações `{add, remove}` relativas ao que estava resolvido ao
    // abrir o editor: o que o dono acrescentou vai em `add`, o que removeu vai em
    // `remove` — a base externa daquele idioma segue viva por baixo (R3).
    const add = tags.filter((t) => !sementes.tags.includes(t))
    const remove = sementes.tags.filter((t) => !tags.includes(t))
    if (add.length > 0 || remove.length > 0) {
      patch.tags = { add, remove }
    }
    return patch
  }

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    const patch = construirPatch()
    if (Object.keys(patch).length === 0) {
      // Nada mudou — sai da edição sem bater na API.
      onDone()
      return
    }
    salvar.mutate(patch)
  }

  function adicionarTag() {
    const limpo = tagInput.trim()
    if (limpo !== '' && !tags.includes(limpo)) {
      setTags([...tags, limpo])
    }
    setTagInput('')
  }

  function onTagKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Enter') {
      e.preventDefault()
      adicionarTag()
    }
  }

  const ocupado = salvar.isPending || limpar.isPending
  const mensagemErro =
    salvar.error instanceof ApiError
      ? salvar.error.message
      : limpar.error instanceof ApiError
        ? limpar.error.message
        : salvar.isError || limpar.isError
          ? 'Não foi possível salvar as alterações.'
          : null

  return (
    <form
      onSubmit={onSubmit}
      className="mt-6 flex flex-col gap-5 rounded-xl bg-card p-4 ring-1 ring-foreground/10"
    >
      {/* Campos traduzíveis — editam o idioma de leitura corrente. */}
      <div className="flex flex-col gap-4">
        <p className="text-sm font-medium">
          Editando em <Badge variant="secondary">{rotuloIdioma(lang)}</Badge>
        </p>

        <label className="flex flex-col gap-1.5 text-sm">
          <span className="font-medium">Título</span>
          <div className="flex items-center gap-2">
            <Input
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder={entity.title}
              aria-label="Título"
            />
            <BotaoLimpar
              rotulo="Limpar título"
              onClick={() => limpar.mutate({ campo: 'title', comLang: true })}
              disabled={ocupado}
            />
          </div>
        </label>

        <label className="flex flex-col gap-1.5 text-sm">
          <span className="font-medium">Descrição</span>
          <textarea
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder={entity.description ?? ''}
            aria-label="Descrição"
            className={CLASSE_TEXTAREA}
          />
          <div>
            <BotaoLimpar
              rotulo="Limpar descrição"
              onClick={() =>
                limpar.mutate({ campo: 'description', comLang: true })
              }
              disabled={ocupado}
            />
          </div>
        </label>

        <div className="flex flex-col gap-1.5 text-sm">
          <span className="font-medium">Tags</span>
          <div className="flex flex-wrap items-center gap-1.5">
            {tags.map((tag) => (
              <Badge key={tag} variant="outline" className="gap-1 pr-1">
                {tag}
                <button
                  type="button"
                  aria-label={`Remover tag ${tag}`}
                  onClick={() => setTags(tags.filter((t) => t !== tag))}
                  className="rounded-full text-muted-foreground hover:text-foreground"
                >
                  <X className="size-3" aria-hidden />
                </button>
              </Badge>
            ))}
            {tags.length === 0 ? (
              <span className="text-xs text-muted-foreground">
                Nenhuma tag.
              </span>
            ) : null}
          </div>
          <div className="flex items-center gap-2">
            <Input
              value={tagInput}
              onChange={(e) => setTagInput(e.target.value)}
              onKeyDown={onTagKeyDown}
              placeholder="Adicionar tag e Enter"
              aria-label="Adicionar tag"
            />
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={adicionarTag}
            >
              <Plus aria-hidden />
              Adicionar
            </Button>
            <BotaoLimpar
              rotulo="Limpar tags"
              onClick={() => limpar.mutate({ campo: 'tags', comLang: true })}
              disabled={ocupado}
            />
          </div>
        </div>
      </div>

      {/* Campos neutros — um valor para todos os idiomas (sem seletor). */}
      <div className="flex flex-col gap-4 border-t border-border pt-4">
        <p className="text-sm text-muted-foreground">
          Campos neutros — valem para todos os idiomas.
        </p>

        <label className="flex flex-col gap-1.5 text-sm">
          <span className="font-medium">Título original</span>
          <div className="flex items-center gap-2">
            <Input
              value={originalTitle}
              onChange={(e) => setOriginalTitle(e.target.value)}
              placeholder={entity.original_title ?? ''}
              aria-label="Título original"
            />
            <BotaoLimpar
              rotulo="Limpar título original"
              onClick={() =>
                limpar.mutate({ campo: 'original_title', comLang: false })
              }
              disabled={ocupado}
            />
          </div>
        </label>

        <label className="flex flex-col gap-1.5 text-sm">
          <span className="font-medium">Data de lançamento</span>
          <div className="flex items-center gap-2">
            <Input
              value={releaseDate}
              onChange={(e) => setReleaseDate(e.target.value)}
              placeholder={entity.release_date ?? 'AAAA-MM-DD'}
              aria-label="Data de lançamento"
            />
            <BotaoLimpar
              rotulo="Limpar data de lançamento"
              onClick={() =>
                limpar.mutate({ campo: 'release_date', comLang: false })
              }
              disabled={ocupado}
            />
          </div>
        </label>

        <label className="flex flex-col gap-1.5 text-sm">
          <span className="font-medium">Sua nota (0–10)</span>
          <div className="flex items-center gap-2">
            <Input
              type="number"
              min={0}
              max={10}
              step={0.1}
              value={localRating}
              onChange={(e) => setLocalRating(e.target.value)}
              placeholder={
                entity.vote_average != null
                  ? `Fonte: ${entity.vote_average.toFixed(1)}`
                  : 'Sem nota'
              }
              aria-label="Sua nota"
              className="max-w-32"
            />
            <BotaoLimpar
              rotulo="Limpar sua nota"
              onClick={() =>
                limpar.mutate({ campo: 'local_rating', comLang: false })
              }
              disabled={ocupado}
            />
          </div>
        </label>
      </div>

      {mensagemErro ? (
        <p role="alert" className="text-sm font-medium text-destructive">
          {mensagemErro}
        </p>
      ) : null}

      <div className="flex items-center gap-2">
        <Button type="submit" disabled={ocupado}>
          {salvar.isPending ? 'Salvando…' : 'Salvar alterações'}
        </Button>
        <Button
          type="button"
          variant="ghost"
          onClick={onDone}
          disabled={ocupado}
        >
          Cancelar
        </Button>
      </div>
    </form>
  )
}

// Botao "Limpar" (volta a herdar). Distinto de esvaziar o input (que ao Salvar
// define vazio). O ícone de borracha reforça a intenção "apagar a edição".
function BotaoLimpar({
  rotulo,
  onClick,
  disabled,
}: {
  rotulo: string
  onClick: () => void
  disabled?: boolean
}) {
  return (
    <Button
      type="button"
      variant="ghost"
      size="sm"
      aria-label={rotulo}
      onClick={onClick}
      disabled={disabled}
    >
      <Eraser aria-hidden />
      Limpar
    </Button>
  )
}
