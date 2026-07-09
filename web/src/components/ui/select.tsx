import * as React from 'react'

import { cn } from '@/lib/utils'

// Select nativo estilizado — leve, acessível e trivial de testar (userEvent
// `selectOptions`). Os componentes deste diretório são tratados como gerados
// (excludes de cobertura / react-refresh, padrão do 0005).
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
