import { Link } from '@tanstack/react-router'
import { ArrowLeft } from 'lucide-react'

// Cabeçalho compartilhado por `/library/new` e `/library/$id/edit` (briefing
// 0071): link de volta + título — as duas rotas só diferem no texto do título.
export function LibraryFormHeader({ titulo }: { titulo: string }) {
  return (
    <>
      <Link
        to="/library"
        className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
      >
        <ArrowLeft className="size-4" aria-hidden />
        Voltar às bibliotecas
      </Link>
      <h1 className="mt-4 mb-4 font-heading text-xl font-semibold">{titulo}</h1>
    </>
  )
}
