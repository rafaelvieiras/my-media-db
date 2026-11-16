import { Link } from '@tanstack/react-router'
import { ArrowLeft } from 'lucide-react'

// Header shared by `/dashboard/library/new` and
// `/dashboard/library/$id/edit`: back link + title — the
// two routes only differ in the title text.
export function LibraryFormHeader({ titulo }: { titulo: string }) {
  return (
    <>
      <Link
        to="/dashboard/library"
        className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
      >
        <ArrowLeft className="size-4" aria-hidden />
        Voltar às bibliotecas
      </Link>
      <h1 className="mt-4 mb-4 font-heading text-xl font-semibold">{titulo}</h1>
    </>
  )
}
