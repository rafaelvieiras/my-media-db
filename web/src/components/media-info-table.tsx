import { useQuery } from '@tanstack/react-query'
import { Badge } from '@/components/ui/badge'
import { entityFilesQueryOptions } from '@/lib/queries'
import type { MediaFile } from '@/lib/api'
import {
  chipsDoArquivo,
  descreverAudio,
  descreverLegenda,
  linhaGeral,
} from '@/lib/mediainfo'
import { ErrorState, LoadingState } from '@/components/query-states'
import { useT } from '@/lib/i18n'

// File name from the full path (basename), tolerant of `/` and `\`.
function nomeArquivo(path: string): string {
  const partes = path.split(/[/\\]/)
  return partes[partes.length - 1] || path
}

// Lists the files of the entity with the mediainfo formatted readably
// (resolution · codec · HDR · channels, audio/subtitle tracks, duration, size),
// plus edition/part and state (present/missing).
export function MediaInfoTable({ entityId }: { entityId: string }) {
  const t = useT()
  const { data, isPending, isError, error } = useQuery(
    entityFilesQueryOptions(entityId),
  )

  if (isPending) {
    return <LoadingState label={t('media.files.loading')} />
  }
  if (isError) {
    return (
      <ErrorState
        title={t('media.files.loadError')}
        message={error instanceof Error ? error.message : undefined}
      />
    )
  }
  if (data.items.length === 0) {
    return (
      <p className="py-6 text-sm text-muted-foreground">
        {t('media.files.empty')}
      </p>
    )
  }

  return (
    <ul className="flex flex-col gap-3">
      {data.items.map((file) => (
        <FileRow key={file.id} file={file} />
      ))}
    </ul>
  )
}

function FileRow({ file }: { file: MediaFile }) {
  const t = useT()
  const chips = chipsDoArquivo(file.mediainfo)
  const geral = linhaGeral(file.mediainfo)
  const audios = file.mediainfo?.audio ?? []
  const legendas = file.mediainfo?.subtitle ?? []

  return (
    <li className="flex flex-col gap-2 rounded-xl bg-card p-4 ring-1 ring-foreground/10">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="min-w-0 truncate font-medium" title={file.path}>
          {nomeArquivo(file.path)}
        </span>
        <div className="flex shrink-0 gap-1">
          {file.edition ? (
            <Badge variant="outline">{file.edition}</Badge>
          ) : null}
          {file.part_no !== null ? (
            <Badge variant="outline">
              {t('media.files.part', { n: file.part_no })}
            </Badge>
          ) : null}
          {file.state === 'missing' ? (
            <Badge variant="destructive">{t('media.files.missing')}</Badge>
          ) : (
            <Badge variant="secondary">{t('media.files.present')}</Badge>
          )}
        </div>
      </div>

      {chips.length > 0 ? (
        <p className="text-sm">{chips.join(' · ')}</p>
      ) : (
        <p className="text-sm text-muted-foreground">
          {t('media.files.noTechnicalInfo')}
        </p>
      )}

      {geral.length > 0 ? (
        <p className="text-xs text-muted-foreground">{geral.join(' · ')}</p>
      ) : null}

      {audios.length > 0 ? (
        <p className="text-xs text-muted-foreground">
          <span className="font-medium">{t('media.files.audio')}</span>{' '}
          {audios.map((a) => descreverAudio(a)).join(' / ')}
        </p>
      ) : null}

      {legendas.length > 0 ? (
        <p className="text-xs text-muted-foreground">
          <span className="font-medium">{t('media.files.subtitles')}</span>{' '}
          {legendas.map((s) => descreverLegenda(s)).join(' / ')}
        </p>
      ) : null}
    </li>
  )
}
