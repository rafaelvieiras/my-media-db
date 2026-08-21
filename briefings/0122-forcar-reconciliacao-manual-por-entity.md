# 0122 — Forçar reconciliação manual de UMA entity, ignorando o throttle

> O throttle de 1x/dia da reconciliação automática (briefing 0120) protege o
> job em lote de bater à toa na fonte externa — mas deixa o dono sem
> alternativa quando ele quer feedback IMEDIATO depois de corrigir algo (ex.:
> um bug no algoritmo de match, ou uma tag do próprio arquivo). Fecha esse
> buraco com uma ação pontual, no mesmo padrão já usado por `enrich-missing`/
> `download-missing-artwork` (briefing 0059): sob demanda, por entity, sem
> guarda de elegibilidade nem throttle.

| | |
|---|---|
| **ID** | 0122 |
| **Tipo** | feature |
| **Fase (roadmap)** | Transversal / UX operacional — achado ao validar o 0121 em produção (o throttle do 0120 bloqueou um retry manual no mesmo dia). |
| **Docs relacionadas** | — |
| **Status** | in-progress |
| **Depende de** | [0059](0059-acoes-sob-demanda-item-aberto-enrich-missing-download-artwork.md), [0120](0120-reconciliacao-de-filhos-orfaos-musica-serie.md) |

---

## 1. Objetivo

Ao validar o 0121 em produção (rodada 2, teto de releases + duração como
sinal de confirmação), não foi possível confirmar se a correção resolveu o
caso real (faixa "Freestyler" do artista "Bomfunk MC's") porque
`entity.last_external_match_attempt_at` já tinha sido carimbado pela
validação da rodada 1, mais cedo no mesmo dia — o throttle do briefing 0120
(`retry_days`, mínimo 1 dia corrido) impede uma nova tentativa da
reconciliação automática (`ReconciliarFaixaDraft`/`ReconciliarSerieDraft`,
via `ReconciliarFilhosOrfaos`, rodada do job `saude_biblioteca`) antes disso.

O throttle em si continua correto para o fluxo **automático**: o job roda
1x/dia (`schedule_cron: 0 4 * * *`), então `retry_days=1` na prática só
espelha a própria cadência do cron, sem impor restrição adicional — e evita
gastar cota de rate-limit (MusicBrainz, ~1 req/s, **compartilhado entre todos
os usuários** deste selfhosted) revarrendo, todo dia, entities que
genuinamente continuam sem resposta.

O gap é a ausência de uma **válvula de escape manual**: o dono (ou qualquer
um dos 5-10 usuários administradores desta instância) abre o detalhe de uma
faixa/série presa em rascunho, quer tentar de novo NA HORA (ex.: acabou de
corrigir uma tag, ou sabe que uma correção no backend foi publicada) e hoje
não tem como — só esperando o próximo `saude_biblioteca` (04:00) depois de
passadas as 24h. Diferente do tinyMediaManager (desktop, um usuário, tudo
manual/síncrono): aqui o fluxo é automático em background por padrão, mas
precisa de uma ação explícita pontual pra quando o automático não é
suficiente.

## 2. Decisão

Um endpoint novo, per-entity, seguindo EXATAMENTE o padrão já estabelecido
pelo briefing 0059 (`enrich-missing`/`download-missing-artwork`): dispara a
MESMA lógica da reconciliação em lote (`Manager.ReconciliarFaixaDraft`/
`Manager.ReconciliarSerieDraft`, `internal/scanner/reconciliar.go`), mas para
UM item, **ignorando completamente o throttle** (nunca lê/filtra por
`last_external_match_attempt_at` antes de tentar) — e carimba a tentativa ao
final de qualquer jeito, para coordenar com o próximo lote do job (mesma
justificativa do `enrichMissing`: "para o PRÓXIMO lote do job não competir
pelo item que o dono acabou de resolver manualmente").

Dispatch por `entity.type`: `"faixa"` → `ReconciliarFaixaDraft`;
`"tv"` → `ReconciliarSerieDraft`; qualquer outro tipo → no-op (200,
`{"linked": false}`, SEM carimbar — não houve tentativa real de nada). Nem
`ReconciliarFaixaDraft` nem `ReconciliarSerieDraft` fazem guarda própria de
`status == draft` (conferido no código atual) — chamar num item já
`cataloged`/já vinculado é inofensivo (na pior hipótese, `linked=false` sem
efeito), então o endpoint não precisa duplicar essa checagem; a UI é que
só mostra o botão quando faz sentido (§4).

Localização da ação na UI: o dropdown **"Gerenciar"** do detalhe da entity
(`EntityManageMenu`, `web/src/components/entity-manage-menu.tsx`) — o MESMO
lugar de `enrichMissing`/`downloadMissingArtwork`/`refreshEntityFiles`, não
uma tela nova. Considerado e descartado: a fila `/orphans` (0018) é sobre
entities cujo ARQUIVO sumiu — conceito diferente, ação só de purga; a fila
`/review` (fila de revisão) já tem "Confirmar candidato"/"Corrigir" (troca de
identidade), que é uma decisão diferente de "religar este filho já
identificado contra o pai" — colocar mais um botão lá dilui os dois fluxos.
O detalhe já é para onde "Ver detalhe" leva a partir da fila de revisão, e é
onde toda ação individual sob demanda já mora (briefing 0059/0088).

## 3. Fora deste briefing

- Mudar o valor do throttle (`retry_days`) ou o `schedule_cron` do job — não
  faz parte deste gap, e já está exposto via `/dashboard/settings`
  (retroativo).
- Endpoint em lote para forçar reconciliação de MÚLTIPLAS entities de uma vez
  (o `enrichMissing`/`downloadMissingArtwork` também não têm variante em
  lote própria — a fila de revisão já tem "Auto-sincronizar elegíveis" para
  o caso batch, que é um fluxo diferente, de auto-match, não de
  reconciliação pai-filho).
- Mudar `ReconciliarFaixaDraft`/`ReconciliarSerieDraft` em si — este briefing
  só expõe a mesma função já existente via um caminho novo (sem throttle),
  não muda a lógica de match.

## 4. Contrato técnico

### Backend

- `internal/server/reconciliar_orfaos.go` (mesmo arquivo da rodada em lote,
  para reusar o helper): nova função exportada
  `func (s *Server) forcarReconciliacao(w http.ResponseWriter, r *http.Request)`,
  handler HTTP de `POST /api/v1/entities/{id}/force-reconcile`:
  1. `s.entitiesUnavailable(w)` (mesma guarda de todo handler de entity).
  2. `ent, ok := s.exigirEntity(w, ctx, id)` — 404 se não existe.
  3. `langCfg, err := s.obterLanguageSettings(ctx)` — 500 em erro real.
  4. Dispatch por `ent.Type`:
     - `"faixa"`: `linked, tentarErr := s.scanner.ReconciliarFaixaDraft(ctx, ent.ID, langCfg.Allowed)`.
     - `"tv"`: `linked, tentarErr := s.scanner.ReconciliarSerieDraft(ctx, ent.ID, langCfg.Allowed)`.
     - outro tipo: responde `200 {"linked": false}` direto, SEM chamar nada
       e SEM carimbar (não passa pelo passo 5).
  5. Nos dois primeiros casos, **sempre** chama
     `s.carimbarTentativaExterna(ctx, ent.ID)` ao final (mesmo em erro de
     `tentarErr`, mesma semântica de `enrichMissing`). `tentarErr != nil` OU
     erro do carimbo → 500 (erro real de banco/infra, não "não achou
     candidato" — mesma distinção de `enrichMissing`).
  6. Corpo de resposta (200): `{"linked": bool}` — sem DTO próprio novo, tipo
     simples o bastante para não precisar de struct exportado extra (mas
     seguir o estilo do arquivo: um `type forcarReconciliacaoResultDTO struct
     { Linked bool \`json:"linked"\` }` para bater o padrão de
     `enrichMissingResultDTO`).
  7. `s.scanner == nil` (sem scanner configurado) → mesma guarda de
     `ReconciliarFilhosOrfaos`: responde `200 {"linked": false}` sem tentar
     nada (degrada, não quebra).
  - Sem guarda de status/elegibilidade adicional (§2) — só dispatch por tipo.

- `internal/server/server.go`: registrar a rota no MESMO grupo de
  `enrich-missing`/`download-missing-artwork` (perto da linha ~790):
  ```go
  ent.Post("/{id}/force-reconcile", s.forcarReconciliacao)
  ```

- `openapi.yaml`: novo path `/api/v1/entities/{id}/force-reconcile` (`POST`,
  `operationId: forceReconcile`), descrição citando o briefing 0122 e a
  diferença para `enrich-missing` (aqui é reconciliação pai-filho, não
  identidade externa); novo schema `ForceReconcileResult { linked: boolean }`;
  respostas 200/404/500, mesmo padrão de `enrich-missing`.

### Frontend

- `web/src/lib/api.ts` (+ `api-types.ts`): nova função `forceReconcile(id:
  string): Promise<ForceReconcileResult>` chamando
  `POST /api/v1/entities/{id}/force-reconcile`, mesmo padrão de
  `enrichMissing`/`downloadMissingArtwork` (gerar/ajustar os tipos a partir do
  `openapi.yaml`, `npm run gen:types`, conferir que saiu formatado).

- `web/src/components/entity-manage-menu.tsx`:
  - `EntityManageMenu` ganha duas props novas: `type: string` e
    `status: string` (a entity já tem os dois no DTO do detalhe,
    `data.type`/`data.status` em `entity-detail.tsx`).
  - Item de menu novo, condicional:
    `mostrarReconciliar = (type === 'faixa' || type === 'tv') &&
    ehDraft(status)` (reusa o helper `ehDraft` já importado em outros
    lugares do projeto, `@/lib/entity-badges`) — só aparece quando faz
    sentido, análogo a `mostrarInformacoes`/`mostrarArte`.
  - Mutation `forcarReconciliar = useMutation({ mutationFn: () =>
    forceReconcile(id), onSuccess: invalidar })` (mesma função `invalidar`
    já existente no componente — a entity muda de `status` quando religada).
  - Mensagem de resultado, mesmo padrão dos outros itens do menu (`role
    "status"`/`"alert"` abaixo do dropdown): `linked === true` → "Religado
    com sucesso — item saiu do rascunho."; `linked === false` → "Não foi
    possível religar desta vez — o item continua em rascunho."; erro de rede
    → mensagem de `ApiError` (mesmo padrão de `erroEnriquecer` etc.).
  - `entity-detail.tsx`: passar as duas props novas
    (`type={data.type}`/`status={data.status}`) na chamada de
    `<EntityManageMenu ... />`.

## 5. Testes

- Backend (`internal/server/reconciliar_orfaos_test.go` ou um novo arquivo
  `force_reconcile_test.go` no mesmo pacote): cobrir (a) faixa draft com
  `last_external_match_attempt_at` recente (dentro do throttle) ainda assim
  tenta e retorna `linked` conforme o resultado — prova que o throttle é
  ignorado; (b) série draft equivalente; (c) tipo não aplicável (`movie`) →
  `200 {"linked": false}`, sem carimbar (assert direto no banco/mock); (d)
  entity inexistente → 404; (e) `s.scanner == nil` → `200 {"linked":
  false}`; (f) erro de `tentar`/carimbo → 500.
- Frontend (`entity-manage-menu.test.tsx`): (a) item "Forçar reconciliação"
  aparece só quando `type` é faixa/tv E `status` é draft; (b) clique chama
  `forceReconcile` e mostra a mensagem de sucesso/insucesso conforme
  `linked`; (c) erro de rede mostra a mensagem de `ApiError`.
- E2E (Playwright, se o fluxo de música/série já tiver um cenário
  equivalente de item em rascunho no ambiente E2E — senão, cobertura via
  Camada 1 (Go) já é suficiente para este briefing, mesmo padrão dos outros
  itens do menu que não têm E2E dedicado).

## 6. Critérios de aceite

- [x] `POST /entities/{id}/force-reconcile` implementado, ignorando o
      throttle, carimbando a tentativa (faixa/série) ao final.
- [x] Tipo não aplicável / sem scanner / entity inexistente tratados sem
      5xx (exceto erro real de banco/infra).
- [x] Botão "Forçar reconciliação" no menu "Gerenciar", visível só para
      faixa/série em rascunho.
- [x] `openapi.yaml` + tipos gerados do frontend batendo com o contrato.
- [x] `go build`/`go vet`/`go test ./... -count=1`, `tsc -b`, `npm run
      lint`, `make gate-local` verdes.
- [ ] Validado em produção: usar o botão na entity real do Bomfunk MC's/
      Freestyler (`4838fee1-8b95-4d72-8142-e4edaa106975`) e confirmar se a
      correção do briefing 0121 (rodada 2) resolveu o caso real, sem
      depender do throttle — fecha o item que ficou em aberto no 0121.
      **Validado em 21/08/2026 (deploy do commit `d2d6a6c`, versão exibida
      no log de boot): o mecanismo deste briefing funciona — o endpoint
      ignorou o throttle e fez uma tentativa real (~40s, chamada de rede
      de fato disparada), confirmado pelo log
      `POST .../force-reconcile ... 200 17B in 40.415582778s`. Porém o
      resultado foi `{"linked":false}` e a entity permanece `status:
      "draft"`. O log do scanner aponta a causa raiz, e não é mais o
      throttle: `scanner: import escopado (busca textual artista
      "Bomfunk MC's" album "Freestyler") falhou: enrich: faixa alvo nao
      encontrada no album 1ca542d9-eec9-4bad-9e20-7535431f5b1c` — ou
      seja, a correção do 0121 (rodada 2, teto de releases/duração como
      confirmação) não resolveu o caso real: o MusicBrainz encontra o
      álbum mas não localiza a faixa-alvo dentro dele. Este item
      permanece em aberto; precisa de um briefing de investigação novo
      sobre o matching faixa↔álbum do provider MusicBrainz para este
      release-group específico.**
