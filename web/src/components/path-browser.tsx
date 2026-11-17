import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ArrowUp, ChevronRight, Folder, FolderSearch, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { ApiError } from '@/lib/api'
import { libraryBrowseQueryOptions } from '@/lib/queries'
import { cn } from '@/lib/utils'
import { useT } from '@/lib/i18n'

const RAIZ = '/'

function pareceAbsoluto(v: string): boolean {
  return v.trim().startsWith('/')
}

export interface PathBrowserProps {
  /** Current value of the text field (already existing) of the calling form. */
  value: string
  /** Fired when the owner chooses "Usar esta pasta". */
  onChange: (path: string) => void
}

// Jellyfin-style folder browser: an EXPANDABLE complement to the
// free text field already existing in the form — it does NOT replace it, it is a visual
// shortcut for the same space of paths that the free text already allowed typing.
// It starts collapsed behind "Procurar pasta…" (the user journey described in the spec).
//
// Synchronization with the typed text: since this component does not own the
// `<Input>` (it lives in the calling route), the only way to react to "type and
// leave the field" is to observe the `value` prop — we use a short debounce (instead of
// an explicit blur event, which we cannot listen to from here) to try to
// navigate to what was typed, only when the browser is open and the value
// looks like an absolute path. If the navigation fails (invalid/unreadable path), the
// browser silently goes back to the last valid folder — the text already typed
// is never touched by this component (it does not have that power: the owner is the
// `<Input>` of the caller).
export function PathBrowser({ value, onChange }: PathBrowserProps) {
  const t = useT()
  const [aberto, setAberto] = useState(false)
  const [pasta, setPasta] = useState(() =>
    pareceAbsoluto(value) ? value.trim() : RAIZ,
  )
  // It starts at the ROOT (not at the initial `pasta`, which may be the broken path of
  // an offline source): if the first load already errors, the recovery effect
  // below has somewhere to go back to instead of getting stuck without any
  // data at all.
  const ultimaValida = useRef(RAIZ)
  // Tracks the LAST `value` (text of the caller field) already synchronized —
  // without that, the debounce below fires every time `pasta` diverges from
  // `value` for ANY reason (e.g. the owner navigating manually through the
  // browser), and not only when the owner actually EDITED the text field —
  // overwriting the manual navigation 400ms later and locking the browser on the
  // old path (the reported bug: no level of the breadcrumb "catches").
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

  // Invalid path (typed or navigated) — it goes back to the last folder that loaded
  // successfully, without retrying in a loop (it only fires when `pasta` still
  // differs from the last known valid one).
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
        {t('jobs.pathBrowser.open')}
      </Button>
    )
  }

  const mensagemErro =
    query.error instanceof ApiError
      ? query.error.message
      : query.isError
        ? t('jobs.pathBrowser.loadError')
        : null

  return (
    <div className="flex flex-col gap-2 rounded-xl bg-card p-3 ring-1 ring-foreground/10">
      <div className="flex items-start justify-between gap-2">
        <Breadcrumb path={query.data?.path ?? pasta} onNavigate={setPasta} />
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label={t('jobs.pathBrowser.close')}
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
          {t('jobs.pathBrowser.up')}
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
          {t('jobs.pathBrowser.useThisFolder')}
        </Button>
      </div>

      {query.isPending ? (
        <p role="status" className="text-sm text-muted-foreground">
          {t('jobs.pathBrowser.loading')}
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
                {t('jobs.pathBrowser.empty')}
              </li>
            ) : (
              query.data.entries.map((entry) => (
                <li key={entry.path}>
                  <button
                    type="button"
                    disabled={!entry.readable}
                    title={
                      entry.readable
                        ? undefined
                        : t('jobs.pathBrowser.noReadPermission')
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
              {t('jobs.pathBrowser.truncated')}
            </p>
          ) : null}
        </>
      )}
    </div>
  )
}

// Clickable breadcrumb of the segments of the current path — each segment (including
// the root) navigates straight there.
function Breadcrumb({
  path,
  onNavigate,
}: {
  path: string
  onNavigate: (path: string) => void
}) {
  const t = useT()
  const partes = path.split('/').filter(Boolean)
  const crumbs: { label: string; path: string }[] = [{ label: '/', path: '/' }]
  let acumulado = ''
  for (const parte of partes) {
    acumulado += `/${parte}`
    crumbs.push({ label: parte, path: acumulado })
  }

  return (
    <nav
      aria-label={t('jobs.pathBrowser.currentPath')}
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
