import { useEffect, useState } from 'react'

// useDebounced delays the propagation of a value: it only returns the new `value` after `ms`
// without changes (it cancels the timer on every keystroke). Single source of the typing
// debounce reused by the search fields (search panel, match correction, collection
// member picker) — avoids repeating the same `useState`+`useEffect`+timeout on every
// screen (the ratchet measures duplication with jscpd).
export function useDebounced<T>(value: T, ms: number): T {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const t = setTimeout(() => setDebounced(value), ms)
    return () => clearTimeout(t)
  }, [value, ms])
  return debounced
}
