# 0073 — Guard de "sair sem salvar" na edição de item

> A rota `/entities/$id/edit` tem 3 blocos de salvamento independentes; sair
> sem salvar um deles hoje descarta a edição sem nenhum aviso.

| | |
|---|---|
| **ID** | 0073 |
| **Tipo** | fix |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Status** | done |
| **Docs relacionadas** | [04-modelo-de-dados](../docs/04-modelo-de-dados.md) |
| **Depende de** | 0016, 0027, 0029, 0030 |

---

## 1. Objetivo

A auditoria de UX confirmou, navegando de verdade: `/entities/$id/edit`
(briefing 0030) já é uma rota dedicada — não modal, no padrão que o time
adotou. Mas dentro dela existem **três** áreas com salvamento próprio e
independente:

1. O card de topo (`SchemaForm`: Título/Sinopse/Tags/Título original/Data/Nota
   → "Salvar alterações").
2. `ExternalIdsEditor` (IDs externos → "Salvar IDs"/"Salvar e re-enriquecer").
3. `CreditsEditor` (elenco+equipe+produtoras → "Salvar créditos").

Teste ao vivo: editar o Título e clicar no link "Voltar ao item" (que fica no
topo da página, fora de qualquer um dos três blocos) sem clicar em "Salvar
alterações" descarta a edição **silenciosamente** — sem confirmação, sem
aviso, sem indício de que algo foi perdido. É fácil editar dois blocos na
mesma visita (ex.: corrigir o título e reordenar o elenco) e sair achando que
salvou tudo porque só um botão foi clicado. Isso é exatamente o tipo de perda
de estado que a migração de modal para rota dedicada (briefing 0071, aplicada
em `/library`) tentou evitar — a rota existe, mas o salvamento fragmentado
reabre o mesmo risco por outra porta.

## 2. Jornada do usuário

1. O dono abre `/entities/$id/edit`, edita o Título no card de topo.
2. Sem clicar em "Salvar alterações", clica no link "Voltar ao item" (topo da
   página) ou navega para outra rota (ex.: fecha a aba, aperta Alt+Seta).
3. **Hoje**: a navegação acontece na hora, a edição é perdida sem aviso.
4. **Depois deste briefing**: aparece uma confirmação explícita ("Você tem
   alterações não salvas neste item. Sair mesmo assim?") antes de navegar para
   fora da rota de edição, cobrindo os três blocos (qualquer um com alteração
   pendente dispara o aviso). Fechar a aba/recarregar (evento nativo do
   navegador) também avisa, via `beforeunload`.
5. Confirmando a saída, a navegação prossegue normalmente (o comportamento
   atual "descarta e sai" continua existindo — só passa a ser uma escolha
   explícita, não um acidente).

## 3. Escopo

**Dentro:**
- Rastrear estado "sujo" (alterações não salvas) dos três blocos existentes
  em `/entities/$id/edit`: `SchemaForm`, `ExternalIdsEditor`, `CreditsEditor`.
- Interceptar a navegação PARA FORA da rota de edição (o link "Voltar ao
  item" e qualquer navegação do router) quando algum bloco estiver sujo, com
  uma confirmação explícita (`window.confirm` é aceitável aqui — é um guard
  de perda de dados, não uma ação destrutiva do domínio; ou um
  `alertdialog` do design system, se ficar mais barato dado o mecanismo de
  interceptação escolhido).
- `beforeunload` nativo quando algum bloco estiver sujo (cobre fechar
  aba/recarregar, que o router não intercepta).

**Fora (explicitamente):**
- Unificar os três blocos num único formulário/salvamento (os três têm ciclos
  de vida genuinamente distintos — override por campo, identidade externa com
  re-enriquecimento, coleção grande de créditos — e a auditoria não pediu
  fundi-los, só avisar antes de perder o que não foi salvo).
- Auto-save ou salvamento periódico.
- Qualquer mudança na reordenação de elenco/equipe (achado separado, ver
  [0076](0076-creditos-reordenar-em-lote-e-fotos-de-equipe.md)) — este
  briefing só lê o estado "sujo" do `CreditsEditor`, não mexe em como ele
  reordena.

## 4. Contrato técnico

### Frontend (Vite/React)

- `web/src/routes/entity-edit.tsx` (`EntityEditScreen`/`EntityEditForm`):
  - Precisa saber se `SchemaForm` tem alterações pendentes. `SchemaForm` já
    usa TanStack Form internamente (ver `web/src/components/schema-form.tsx`)
    — expor um `onDirtyChange(sujo: boolean)` (ou devolver `formApi.state.isDirty`
    via callback) é o caminho mais barato, sem duplicar a lógica de comparação
    de valores que o form já faz.
  - `ExternalIdsEditor` e `CreditsEditor` precisam do mesmo contrato:
    `onDirtyChange(sujo: boolean)` (prop nova em cada um), refletindo se há
    edição feita desde o último carregamento/salvamento bem-sucedido daquele
    bloco especificamente.
  - `EntityEditForm` combina os três booleanos (`algumSujo = schemaSujo ||
    idsSujo || creditosSujo`) num estado local.
  - O link "Voltar ao item" (hoje um `<Link>` simples) passa a checar
    `algumSujo` antes de navegar: se sujo, confirma (`window.confirm(...)` ou
    `alertdialog`); se confirmado (ou não sujo), navega normalmente.
  - `useEffect` registra `window.addEventListener('beforeunload', ...)`
    enquanto `algumSujo` for `true` (padrão: `e.preventDefault(); e.returnValue
    = ''`), removendo o listener ao desmontar/quando `algumSujo` virar
    `false`.
- `web/src/components/schema-form.tsx`, `external-ids-editor.tsx`,
  `credits-editor.tsx`: adicionar a prop `onDirtyChange` a cada um, chamada
  sempre que o estado "sujo" mudar (não a cada keystroke sem necessidade —
  só na transição true/false, para não gerar renders extras no pai).

### Arquivos afetados

- `web/src/routes/entity-edit.tsx` (+ `.test.tsx`).
- `web/src/components/schema-form.tsx` (+ `.test.tsx`).
- `web/src/components/external-ids-editor.tsx` (+ `.test.tsx`).
- `web/src/components/credits-editor.tsx` (+ `.test.tsx`).

## 5. Critérios de aceite

- [ ] Editar o Título (ou qualquer campo do card de topo) sem salvar e clicar
      "Voltar ao item" dispara uma confirmação explícita; cancelando a
      confirmação, o dono permanece na tela de edição com a alteração intacta.
- [ ] O mesmo vale editando só o bloco de IDs externos, ou só reordenando o
      elenco/equipe (sem salvar), sem tocar nos outros blocos.
- [ ] Sem nenhuma alteração pendente em nenhum dos três blocos, "Voltar ao
      item" navega direto, sem confirmação.
- [ ] Salvar um bloco (ex.: "Salvar alterações") zera o estado "sujo" DAQUELE
      bloco especificamente — se os outros dois continuam sujos, a
      confirmação ainda dispara ao sair.
- [ ] Fechar a aba/recarregar com alguma alteração pendente dispara o aviso
      nativo do navegador (`beforeunload`).
- [ ] `make e2e-cov` e `make gate-local` passam.

## 6. Notas de implementação / decisões

- **Por que não unificar os três blocos num único "Salvar tudo"?** Cada bloco
  já tem uma ação de salvamento com efeito colateral distinto no backend
  (override por campo/idioma; identidade externa + re-enriquecimento
  opcional; substituição do bloco de créditos inteiro). Unificar exigiria
  coordenar três mutações com falhas parciais possíveis (ex.: IDs salva,
  créditos falha) — escopo maior do que o problema relatado (perda
  silenciosa), que o guard já resolve sem essa complexidade.
- **`window.confirm` é aceitável aqui?** Diferente de uma ação destrutiva do
  domínio (remover datasource, purgar órfã — que usam `alertdialog` em 2
  passos), este é um guard de navegação padrão do próprio navegador
  (`beforeunload` É `window.confirm`-like por natureza). Usar o mesmo
  mecanismo simples para o clique em "Voltar ao item" mantém consistência
  interna sem inventar um segundo padrão de confirmação só para isso — mas se
  a implementação achar barato reusar o `alertdialog` do design system para
  esse caso específico (clique no link, não o evento nativo), não há
  objeção; o critério de aceite é o comportamento, não o componente exato.

## 7. Fora deste briefing / follow-ups

- Melhorias na própria reordenação de elenco/equipe e fotos de equipe —
  [0076](0076-creditos-reordenar-em-lote-e-fotos-de-equipe.md). Como ambos
  tocam `credits-editor.tsx`, recomenda-se aterrissar este briefing (0073)
  primeiro para reduzir o risco de conflito de merge entre os dois.
