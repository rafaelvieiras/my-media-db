import { useEffect, useState } from 'react'

// useDebounced atrasa a propagação de um valor: só devolve o novo `value` após `ms`
// sem mudanças (cancela o timer a cada tecla). Fonte única do debounce de digitação
// reusada pelos campos de busca (painel de busca, correção de match, picker de
// membros da coleção) — evita repetir o mesmo `useState`+`useEffect`+timeout em cada
// tela (a catraca mede duplicação com jscpd).
export function useDebounced<T>(value: T, ms: number): T {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const t = setTimeout(() => setDebounced(value), ms)
    return () => clearTimeout(t)
  }, [value, ms])
  return debounced
}
