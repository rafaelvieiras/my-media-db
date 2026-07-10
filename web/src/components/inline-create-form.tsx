import { useState, type FormEvent, type ReactNode } from 'react'
import { X } from 'lucide-react'
import { Button } from '@/components/ui/button'

// Scaffold COMPARTILHADO do "criar do zero" (0025 coleção, 0027 item): sem primitivo de
// Dialog no projeto, cada criação é um botão que expande um formulário INLINE. Toggle e
// invólucro do form (cabeçalho + cancelar + erro + enviar) viviam duplicados entre
// CollectionCreate e ItemCreate — aqui ficam uma vez só (jscpd). Cada tela injeta apenas
// seus campos e o onSubmit; a mutation "cria e navega" está em lib/use-criar-entidade.

// CreateToggle mostra o botão de abrir e, quando aberto, o formulário (via `children`,
// que recebe o `onClose` para o cancelar/fechar).
export function CreateToggle({
  label,
  icon,
  children,
}: {
  label: string
  icon: ReactNode
  children: (onClose: () => void) => ReactNode
}) {
  const [aberto, setAberto] = useState(false)
  if (!aberto) {
    return (
      <Button variant="outline" size="sm" onClick={() => setAberto(true)}>
        {icon}
        {label}
      </Button>
    )
  }
  return <>{children(() => setAberto(false))}</>
}

// CreateFormShell é o invólucro do formulário inline: cabeçalho com título + cancelar, os
// campos (`children`), o erro inline e o botão de enviar (desabilitado enquanto pendente
// ou sem poder enviar). O onSubmit já faz preventDefault.
export function CreateFormShell({
  title,
  submitLabel,
  submitPendingLabel,
  submitIcon,
  isPending,
  canSubmit,
  mensagemErro,
  onSubmit,
  onClose,
  children,
}: {
  title: string
  submitLabel: string
  submitPendingLabel: string
  submitIcon: ReactNode
  isPending: boolean
  canSubmit: boolean
  mensagemErro: string | null
  onSubmit: () => void
  onClose: () => void
  children: ReactNode
}) {
  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    onSubmit()
  }
  return (
    <form
      onSubmit={handleSubmit}
      className="flex w-full max-w-md flex-col gap-3 rounded-xl bg-card p-4 ring-1 ring-foreground/10"
    >
      <div className="flex items-center justify-between">
        <span className="font-medium">{title}</span>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          aria-label="Cancelar"
          onClick={onClose}
        >
          <X aria-hidden />
        </Button>
      </div>
      {children}
      {mensagemErro ? (
        <p role="alert" className="text-sm font-medium text-destructive">
          {mensagemErro}
        </p>
      ) : null}
      <div className="flex justify-end">
        <Button type="submit" disabled={isPending || !canSubmit}>
          {submitIcon}
          {isPending ? submitPendingLabel : submitLabel}
        </Button>
      </div>
    </form>
  )
}
