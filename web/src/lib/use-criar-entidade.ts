import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { ApiError, type ResolvedEntity } from '@/lib/api'

// useCriarEntidade encapsulates the shared mutation of "create an entity and go to
// it" (inline creation of collection/item): on success it invalidates the grid
// (`['entities']`) and navigates to `/entities/{id}`. Returns the mutation and the error
// message (ApiError) already derived, with a pt-BR fallback. It lives in lib/ (not in the
// components module) so fast-refresh does not complain about mixed exports (components only).
export function useCriarEntidade<TBody>(
  mutationFn: (body: TBody) => Promise<ResolvedEntity>,
  fallbackErro: string,
) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const mutation = useMutation({
    mutationFn,
    onSuccess: (entity: ResolvedEntity) => {
      queryClient.invalidateQueries({ queryKey: ['entities'] })
      navigate({ to: '/entities/$id', params: { id: entity.id } })
    },
  })
  const mensagemErro =
    mutation.error instanceof ApiError
      ? mutation.error.message
      : mutation.isError
        ? fallbackErro
        : null
  return { mutation, mensagemErro }
}
