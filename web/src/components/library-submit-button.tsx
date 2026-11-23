import { type LucideIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'

// Submit button shared by `/dashboard/library/new` and
// `/dashboard/library/$id/edit`: the two routes only differ in the icon and in the
// idle/pending copy — the disabled-while-pending behavior is the same.
export function LibrarySubmitButton({
  Icone,
  pendente,
  rotulo,
  rotuloPendente,
}: {
  Icone: LucideIcon
  pendente: boolean
  rotulo: string
  rotuloPendente: string
}) {
  return (
    <div>
      <Button type="submit" disabled={pendente}>
        <Icone aria-hidden />
        {pendente ? rotuloPendente : rotulo}
      </Button>
    </div>
  )
}
