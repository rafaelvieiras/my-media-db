import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { ApiError, type ResolvedEntity } from '@/lib/api'

// useCriarEntidade encapsula a mutation compartilhada de "criar uma entity e ir para
// ela" (criação inline de coleção/item, 0025/0027): no sucesso invalida o grid
// (`['entities']`) e navega para `/entities/{id}`. Devolve a mutation e a mensagem de erro
// (ApiError) já derivada, com um fallback pt-BR. Fica em lib/ (não no módulo dos
// componentes) para o fast-refresh não reclamar de export misto (só-componentes).
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
