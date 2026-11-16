import * as React from 'react'

import { cn } from '@/lib/utils'

// Styled native select — lightweight, accessible and trivial to test (userEvent
// `selectOptions`). The components of this directory are treated as generated
// (coverage excludes / react-refresh, the standard for UI primitives).
function Select({ className, ...props }: React.ComponentProps<'select'>) {
  return (
    <select
      data-slot="select"
      className={cn(
        'flex h-8 w-full min-w-0 rounded-lg border border-border bg-background px-2.5 py-1 text-sm shadow-xs transition-[color,box-shadow] outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:pointer-events-none disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-3 aria-invalid:ring-destructive/20 dark:bg-input/30',
        className,
      )}
      {...props}
    />
  )
}

export { Select }
