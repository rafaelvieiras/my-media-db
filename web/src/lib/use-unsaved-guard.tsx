import type { ReactNode } from 'react'
import { useBlocker } from '@tanstack/react-router'
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

// "Leave without saving" guard, shared by every form route (`/entities/$id/edit*`,
// settings, integrations and the two creation screens). It was born inside
// `EntityEditShell`; extracting it here is what lets the routes outside `/edit`
// warn too.
//
// It covers the three ways out of a form:
//   - the in-app navigation (any `<Link>` or `navigate()`), through the router
//     blocker, resolved by the `AlertDialog` this hook RENDERS — the caller must
//     put the returned node in its tree (`{useUnsavedGuard(dirty)}`);
//   - the browser/tab close, through `enableBeforeUnload`;
//   - nothing happens while the form is pristine (`dirty` false), so a route that
//     never touched a field navigates freely.
//
// The confirmation is the product's own dialog, not `window.confirm`: the browser can
// suppress the native one, it cannot be translated or styled, and it puts "OK" where the
// verb belongs.
export function useUnsavedGuard(dirty: boolean): ReactNode {
  const t = useT()

  const blocker = useBlocker({
    shouldBlockFn: () => dirty,
    withResolver: true,
    enableBeforeUnload: () => dirty,
    disabled: !dirty,
  })

  if (blocker.status !== 'blocked') {
    return null
  }

  return (
    <AlertDialog
      open
      onOpenChange={(aberto) => {
        if (!aberto) blocker.reset()
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            {t('common.message.unsavedTitle')}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t('common.message.unsavedChanges')}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel onClick={() => blocker.reset()}>
            {t('common.action.cancel')}
          </AlertDialogCancel>
          <AlertDialogAction onClick={() => blocker.proceed()}>
            {t('common.message.leaveWithoutSaving')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
