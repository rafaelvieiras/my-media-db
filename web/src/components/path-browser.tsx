import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ArrowUp, ChevronRight, Folder, FolderSearch, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { ApiError } from '@/lib/api'
import { libraryBrowseQueryOptions } from '@/lib/queries'
import { cn } from '@/lib/utils'

const RAIZ = '/'

function pareceAbsoluto(v: string): boolean {
  return v.trim().startsWith('/')
}

export interface PathBrowserProps {
  /** Valor atual do campo de texto (já existente) do formulário chamador. */
  value: string
  /** Disparado quando o dono escolhe "Usar esta pasta". */
  onChange: (path: string) => void
}

// Navegador de pastas estilo Jellyfin (briefing 0071): complemento EXPANSÍVEL ao
// campo de texto livre já existente no formulário — NÃO o substitui, é um atalho
// visual para o mesmo espaço de caminhos que o texto livre já permitia digitar.
// Começa colapsado atrás de "Procurar pasta…" (a jornada do usuário no briefing).
//
// Sincronização com o texto digitado: como este componente não é dono do
// `<Input>` (ele vive na rota chamadora), a única forma de reagir ao "digitar e
// sair do campo" é observar a prop `value` — usamos um debounce curto (em vez de
// um evento de blur explícito, que não temos como escutar daqui) para tentar
// navegar até o que foi digitado, só quando o navegador está aberto e o valor
// parece um caminho absoluto. Se a navegação falhar (path inválido/ilegível), o
// navegador volta silenciosamente à última pasta válida — o texto já digitado
// nunca é tocado por este componente (ele não tem esse poder: quem o possui é o
// `<Input>` do chamador).
export function PathBrowser({ value, onChange }: PathBrowserProps) {
  const [aberto, setAberto] = useState(false)
  const [pasta, setPasta] = useState(() =>
    pareceAbsoluto(value) ? value.trim() : RAIZ,
  )
  // Começa em RAIZ (não na `pasta` inicial, que pode ser o caminho quebrado de
  // uma fonte offline): se o primeiro carregamento já der erro, o efeito de
  // recuperação abaixo tem para onde voltar em vez de ficar preso sem dado
  // nenhum.
  const ultimaValida = useRef(RAIZ)
  // Rastreia o ÚLTIMO `value` (texto do campo do chamador) já sincronizado —
  // sem isso, o debounce abaixo dispara toda vez que `pasta` diverge de
  // `value` por QUALQUER motivo (ex.: o dono navegando manualmente pelo
  // navegador), e não só quando o dono de fato EDITOU o campo de texto —
  // sobrescrevendo a navegação manual 400ms depois e travando o navegador no
  // caminho antigo (o bug reportado: nenhum nível do breadcrumb "pega").
  const ultimoValueSincronizado = useRef(value)

  useEffect(() => {
    if (value === ultimoValueSincronizado.current) {
      return
    }
    ultimoValueSincronizado.current = value
    if (!aberto || !pareceAbsoluto(value)) {
      return
    }
    const alvo = value.trim()
    const timer = setTimeout(() => setPasta(alvo), 400)
    return () => clearTimeout(timer)
  }, [value, aberto])

  const query = useQuery(libraryBrowseQueryOptions(pasta))

  useEffect(() => {
    if (query.data) {
      ultimaValida.current = query.data.path
    }
  }, [query.data])

  // Path inválido (digitado ou navegado) — volta à última pasta que carregou
  // com sucesso, sem tentar de novo em loop (só dispara quando `pasta` ainda
  // difere da última válida conhecida).
  useEffect(() => {
    if (query.isError && pasta !== ultimaValida.current) {
      setPasta(ultimaValida.current)
    }
  }, [query.isError, pasta])

  if (!aberto) {
    return (
      <Button
        type="button"
        variant="outline"
        size="sm"
        onClick={() => setAberto(true)}
      >
        <FolderSearch aria-hidden />
        Procurar pasta…
      </Button>
    )
  }

  const mensagemErro =
    query.error instanceof ApiError
      ? query.error.message
      : query.isError
        ? 'Não foi possível listar esta pasta.'
        : null

  return (
    <div className="flex flex-col gap-2 rounded-xl bg-card p-3 ring-1 ring-foreground/10">
      <div className="flex items-start justify-between gap-2">
        <Breadcrumb path={query.data?.path ?? pasta} onNavigate={setPasta} />
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label="Fechar navegador de pastas"
          onClick={() => setAberto(false)}
        >
          <X aria-hidden />
        </Button>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={!query.data?.parent}
          onClick={() => {
            if (query.data?.parent) {
              setPasta(query.data.parent)
            }
          }}
        >
          <ArrowUp aria-hidden />
          Subir um nível
        </Button>
        <Button
          type="button"
          size="sm"
          disabled={!query.data}
          onClick={() => {
            if (query.data) {
              onChange(query.data.path)
            }
          }}
        >
          Usar esta pasta
        </Button>
      </div>

      {query.isPending ? (
        <p role="status" className="text-sm text-muted-foreground">
          Carregando pastas…
        </p>
      ) : query.isError ? (
        <p role="alert" className="text-sm font-medium text-destructive">
          {mensagemErro}
        </p>
      ) : (
        <>
          <ul className="flex max-h-56 flex-col gap-0.5 overflow-y-auto">
            {query.data.entries.length === 0 ? (
              <li className="px-2 py-1.5 text-sm text-muted-foreground">
                Nenhuma subpasta aqui.
              </li>
            ) : (
              query.data.entries.map((entry) => (
                <li key={entry.path}>
                  <button
                    type="button"
                    disabled={!entry.readable}
                    title={
                      entry.readable ? undefined : 'sem permissão de leitura'
                    }
                    onClick={() => setPasta(entry.path)}
                    className={cn(
                      'flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left text-sm',
                      entry.readable
                        ? 'hover:bg-muted'
                        : 'cursor-not-allowed text-muted-foreground/50',
                    )}
                  >
                    <Folder className="size-4 shrink-0" aria-hidden />
                    <span className="truncate">{entry.name}</span>
                  </button>
                </li>
              ))
            )}
          </ul>
          {query.data.truncated ? (
            <p className="text-xs text-muted-foreground">
              Mostrando as primeiras 2000 pastas.
            </p>
          ) : null}
        </>
      )}
    </div>
  )
}

// Breadcrumb clicável dos segmentos do caminho atual — cada segmento (inclusive
// a raiz) navega direto para lá.
function Breadcrumb({
  path,
  onNavigate,
}: {
  path: string
  onNavigate: (path: string) => void
}) {
  const partes = path.split('/').filter(Boolean)
  const crumbs: { label: string; path: string }[] = [{ label: '/', path: '/' }]
  let acumulado = ''
  for (const parte of partes) {
    acumulado += `/${parte}`
    crumbs.push({ label: parte, path: acumulado })
  }

  return (
    <nav
      aria-label="Caminho atual"
      className="flex min-w-0 flex-1 flex-wrap items-center gap-0.5 text-sm"
    >
      {crumbs.map((crumb, i) => (
        <span key={crumb.path} className="flex items-center gap-0.5">
          {i > 0 ? (
            <ChevronRight
              className="size-3.5 text-muted-foreground"
              aria-hidden
            />
          ) : null}
          <button
            type="button"
            onClick={() => onNavigate(crumb.path)}
            className="rounded px-1 hover:bg-muted hover:underline"
          >
            {crumb.label}
          </button>
        </span>
      ))}
    </nav>
  )
}
