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

// Nome do arquivo a partir do caminho completo (basename), tolerante a `/` e `\`.
function nomeArquivo(path: string): string {
  const partes = path.split(/[/\\]/)
  return partes[partes.length - 1] || path
}

// Lista os arquivos da entity com o mediainfo formatado de forma legível
// (resolução · codec · HDR · canais, faixas de áudio/legenda, duração, tamanho),
// além de edição/parte e estado (present/missing).
export function MediaInfoTable({ entityId }: { entityId: string }) {
  const { data, isPending, isError, error } = useQuery(
    entityFilesQueryOptions(entityId),
  )

  if (isPending) {
    return <LoadingState label="Carregando arquivos…" />
  }
  if (isError) {
    return (
      <ErrorState
        title="Não foi possível carregar os arquivos"
        message={error instanceof Error ? error.message : undefined}
      />
    )
  }
  if (data.items.length === 0) {
    return (
      <p className="py-6 text-sm text-muted-foreground">
        Nenhum arquivo associado a este item.
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
            <Badge variant="outline">Parte {file.part_no}</Badge>
          ) : null}
          {file.state === 'missing' ? (
            <Badge variant="destructive">ausente</Badge>
          ) : (
            <Badge variant="secondary">presente</Badge>
          )}
        </div>
      </div>

      {chips.length > 0 ? (
        <p className="text-sm">{chips.join(' · ')}</p>
      ) : (
        <p className="text-sm text-muted-foreground">
          Sem informações técnicas
        </p>
      )}

      {geral.length > 0 ? (
        <p className="text-xs text-muted-foreground">{geral.join(' · ')}</p>
      ) : null}

      {audios.length > 0 ? (
        <p className="text-xs text-muted-foreground">
          <span className="font-medium">Áudio: </span>
          {audios.map((a) => descreverAudio(a)).join(' / ')}
        </p>
      ) : null}

      {legendas.length > 0 ? (
        <p className="text-xs text-muted-foreground">
          <span className="font-medium">Legendas: </span>
          {legendas.map((s) => descreverLegenda(s)).join(' / ')}
        </p>
      ) : null}
    </li>
  )
}
