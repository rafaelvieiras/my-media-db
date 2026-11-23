import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate } from '@tanstack/react-router'
import { ArrowLeft } from 'lucide-react'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { useT } from '@/lib/i18n'

// Header shared by `/dashboard/library/new` and
// `/dashboard/library/$id/edit`: back link + title — the
// two routes only differ in the title text.
//
// It also carries the "leave without saving" guard of the two forms (the same
// behavior of `EntityEditShell`): with `dirty` set, the browser asks before a
// reload/close (`beforeunload`) and the back link opens a confirmation instead
// of navigating straight — the edited path used to disappear silently.
export function LibraryFormHeader({
  titulo,
  dirty = false,
}: {
  titulo: string
  // "Leave without saving" guard: does the form hold a change that was not saved?
  dirty?: boolean
}) {
  const t = useT()
  const navigate = useNavigate()
  const [confirmando, setConfirmando] = useState(false)
  // The back link is not a `Button`, so the confirmation keeps the element that
  // triggered it: `onCloseAutoFocus` (Esc/cancel) hands the focus back to it.
  const gatilho = useRef<HTMLAnchorElement | null>(null)

  useEffect(() => {
    if (!dirty) return
    function aoFechar(e: BeforeUnloadEvent) {
      e.preventDefault()
      e.returnValue = ''
    }
    window.addEventListener('beforeunload', aoFechar)
    return () => window.removeEventListener('beforeunload', aoFechar)
  }, [dirty])

  return (
    <>
      <Link
        to="/dashboard/library"
        onClick={(e) => {
          if (!dirty) return
          e.preventDefault()
          gatilho.current = e.currentTarget
          setConfirmando(true)
        }}
        className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
      >
        <ArrowLeft className="size-4" aria-hidden />
        {t('library.form.backToLibraries')}
      </Link>

      <AlertDialog open={confirmando} onOpenChange={setConfirmando}>
        <AlertDialogContent
          onCloseAutoFocus={(e) => {
            // Esc/cancel: the focus goes back to the link that opened the dialog
            // instead of falling into `<body>`.
            e.preventDefault()
            gatilho.current?.focus()
          }}
        >
          <AlertDialogHeader>
            <AlertDialogTitle>{t('library.unsaved.title')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t('common.message.unsavedChanges')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>
              {t('library.unsaved.keepEditing')}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                setConfirmando(false)
                navigate({ to: '/dashboard/library' })
              }}
            >
              {t('library.unsaved.confirm')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <h1 className="mt-4 mb-4 font-heading text-xl font-semibold">{titulo}</h1>
    </>
  )
}
