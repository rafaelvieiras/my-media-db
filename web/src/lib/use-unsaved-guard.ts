import { useBlocker } from '@tanstack/react-router'
import { useT } from '@/lib/i18n'

// "Leave without saving" guard, shared by every form route (`/entities/$id/edit*`,
// settings, integrations and the two creation screens). It was born inside
// `EntityEditShell`; extracting it here is what lets the routes outside `/edit`
// warn too.
//
// It covers the three ways out of a form:
//   - the in-app navigation (any `<Link>` or `navigate()`), through the router
//     blocker — the same confirm copy the shell already used;
//   - the browser/tab close, through `enableBeforeUnload`;
//   - nothing happens while the form is pristine (`dirty` false), so a route that
//     never touched a field navigates freely.
export function useUnsavedGuard(dirty: boolean): void {
  const t = useT()

  useBlocker({
    // A `true` return BLOCKS the navigation, so the confirm answer is inverted.
    shouldBlockFn: () => !window.confirm(t('common.message.unsavedChanges')),
    enableBeforeUnload: () => dirty,
    disabled: !dirty,
  })
}
