# 0085 — Throttle configurável do enriquecimento retroativo (global + por biblioteca)

> O intervalo mínimo entre duas tentativas de enriquecimento retroativo do
> MESMO item (hoje fixo em 30 dias, `internal/server/retroativo.go`) vira
> configurável: um valor GLOBAL editável pelo dono via painel (default 15
> dias) e um override OPCIONAL por `library_source`, mesmo padrão de
> `preferred_language` (0084).

| | |
|---|---|
| **ID** | 0085 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Status** | done |
| **Docs relacionadas** | [06-biblioteca-e-scan](../docs/06-biblioteca-e-scan.md) |
| **Depende de** | 0019 (fundação multi-idioma, molde de `app_settings`), 0056 (enriquecimento retroativo), 0084 (padrão de override por biblioteca) |

---

## 1. Objetivo

O job `saude_biblioteca` (briefing 0056) tenta re-enriquecer, uma vez por
lote diário, itens catalogados sem NENHUM match externo (`match_confidence
== 70`, sintático, nunca verificado contra provider). Para não bater
repetidamente no provider por um item que já foi checado e não achou nada,
existe um throttle por item: `intervaloReTentativaRetroativa = 30 * 24h`
(`internal/server/retroativo.go:41`), hoje uma CONSTANTE de código.

Na prática esse valor fixo prendeu itens "invisíveis" por semanas: um item
cuja ÚNICA tentativa falhou por um motivo TRANSIENTE (provider indisponível,
mount FUSE do Google Drive offline — ver histórico real de
`job_execution.message` com "datasource inacessível (offline)" por 4 dias
seguidos em 02-05/08/2026) só seria retentado automaticamente 30 dias
depois, mesmo quando o candidato certo já está disponível no provider a
qualquer momento (confirmado ao vivo via `match-preview` para 3 itens reais
da biblioteca do dono, todos com score 100 disponível na hora, mas presos
há semanas). Este briefing não muda a LÓGICA de match nem o teto de itens
por execução — só torna o intervalo do throttle uma configuração do dono, em
vez de uma constante enterrada no código.

## 2. Jornada do usuário

1. O dono abre `/dashboard/settings` e vê uma nova seção "Enriquecimento
   retroativo" com um campo numérico "Repetir tentativa após (dias)",
   pré-preenchido com o valor atual (15 por padrão, se nunca configurado).
2. Ao salvar um novo valor (ex.: 7), todas as bibliotecas SEM override
   próprio passam a usar esse intervalo a partir da PRÓXIMA execução do job
   — sem precisar reiniciar o servidor.
3. Ao criar ou editar uma biblioteca (`/dashboard/library/new` ou
   `/dashboard/library/{id}/edit`), o dono vê um campo opcional "Reentativa
   de enriquecimento (dias)" com placeholder "usar padrão global (N dias)"
   (N é o valor global atual, buscado ao carregar o formulário). Deixar em
   branco = herdar o global (comportamento default, igual a hoje exceto
   pelo novo default de 15 em vez de 30).
4. Uma biblioteca com override menor (ex.: 3 dias) tem seus itens
   retentados mais cedo que o resto do catálogo — útil para uma pasta nova,
   ainda sendo povoada, onde o dono quer feedback rápido; uma biblioteca
   "estável" pode receber um override maior para poupar chamadas ao
   provider.
5. O endpoint individual `POST /entities/{id}/enrich-missing` (ação
   explícita do dono num item aberto) continua **sem** nenhum throttle —
   nada muda aí, ver 0056 §6 e §3 abaixo.

## 3. Escopo

**Dentro:**
- Novo recurso em `app_settings`: `retry_days` do enriquecimento
  retroativo, editável via `GET`/`PUT /api/v1/settings/enrichment-retroativo`,
  default 15 (dias) quando nunca configurado. Mesmo molde arquitetural de
  `languages` (0019/`settings.go`): struct própria, cache em memória com
  mutex, invalidado no PUT, `422 invalid_body` para valor fora do intervalo
  válido.
- Novo campo opcional `retroactive_enrichment_days` (nullable) em
  `library_source`, exposto em `POST/PATCH/GET /api/v1/library`, mesmo
  padrão de `preferred_language` (0084): `null`/ausente = herda o global;
  valor presente = override só para itens dessa biblioteca.
- `EnriquecerRetroativo` (`internal/server/retroativo.go`) passa a resolver
  o intervalo aplicável por ENTITY (via a biblioteca de origem do primeiro
  `media_file`, com fallback ao global — ver §4), em vez da constante fixa.
- UI: nova seção em `settings.tsx` (global) e novo campo em
  `library-new.tsx`/`library-edit.tsx` (override).

**Fora (explicitamente):**
- Não muda `limiteEnriquecimentoRetroativoPorExecucao` (teto de 15 itens
  VERIFICADOS por execução do job) nem `limiarSinalizacaoRetroativa` (piso
  de 50.0 para sinalizar à fila de Revisão) — essas duas constantes
  continuam fixas, fora de escopo aqui. **Atenção**: o valor "15" do teto de
  itens por execução é uma constante DIFERENTE e não-relacionada ao novo
  default de "15 dias" do throttle — coincidência de número, não a mesma
  configuração.
- Não adiciona throttle a `POST /entities/{id}/enrich-missing` — esse
  endpoint continua disparando na hora, sem guarda, por design do 0056 §6
  (ação pontual e explícita do dono).
- Não expõe isso via `job_definition.config_json`/`PATCH /jobs/{id}` — hoje
  esse endpoint só edita `enabled`/`schedule_cron` (`internal/server/jobs.go`),
  e `config_json` nunca ficou gravável por HTTP (só via seed/migration).
  Estender essa superfície é uma mudança maior e não pedida; o novo valor
  entra por `app_settings`, caminho já pronto e testado (0019), sem tocar
  no sistema de jobs.
- Não reprocessa retroativamente itens já carimbados só por causa da
  mudança de intervalo — o próximo ciclo natural do job já respeita o novo
  valor (o carimbo antigo simplesmente vence mais cedo ou mais tarde).
- Não adiciona uma tela de "próxima tentativa prevista" por item — fora de
  escopo (poderia ser follow-up, ver §7).

## 4. Contrato técnico

### Dados / modelo
- Migration `internal/db/migrations/0018_library_source_dias_reentativa.sql`:
  ```sql
  ALTER TABLE library_source ADD COLUMN retroactive_enrichment_days INTEGER;
  ```
  Sem `CHECK` — mesmo padrão de `preferred_language` (0084) e `content_type`:
  validação só na camada de aplicação (o intervalo válido é uma regra de
  produto, não de schema). `NULL` = sem override (default).
- `internal/library/library.go`: `Source` ganha
  `RetroactiveEnrichmentDays *int`; `Criar` recebe o novo parâmetro; novo
  método `AtualizarRetroactiveEnrichmentDays` (mesmo padrão de
  `AtualizarPreferredLanguage`).
- `app_settings` ganha a chave `enrichment_retroativo`, valor
  `{"retry_days": N}`. Sem tabela nova — mesmo mecanismo genérico já usado
  por `languages`.

### API (`openapi.yaml`)
- Novo schema `RetroactiveEnrichmentSettings`: `{ retry_days: integer,
  minimum: 1, maximum: 365 }` (campo obrigatório na resposta).
- `GET /api/v1/settings/enrichment-retroativo` → 200
  `RetroactiveEnrichmentSettings` (default `{retry_days: 15}` se a chave
  nunca foi gravada — mesma semântica de "linha ausente ⇒ default seguro"
  de `obterLanguageSettings`).
- `PUT /api/v1/settings/enrichment-retroativo` → corpo
  `RetroactiveEnrichmentSettings`; `200` com o valor gravado; `400
  invalid_body` corpo malformado; `422 invalid_body` `retry_days` fora de
  `[1, 365]`.
- `Source`: novo campo opcional `retroactive_enrichment_days: { type:
  [integer, "null"], minimum: 1, maximum: 365 }`.
- `CreateSource`: novo campo opcional `retroactive_enrichment_days: {
  type: integer, minimum: 1, maximum: 365 }`.
- `PatchSource`: novo campo opcional `retroactive_enrichment_days: {
  type: [integer, "null"], minimum: 1, maximum: 365 }` (`null` explícito
  limpa o override; ausente = inalterado — mesmo padrão de
  `preferred_language` no patch).
- Erros: `422 invalid_body` quando fora de `[1, 365]` em qualquer um dos
  três (`POST`/`PATCH /library`, `PUT /settings/enrichment-retroativo`).

### Backend (Go)
- **`internal/server/settings.go`**: adicionar, ao lado de
  `languageSettings`:
  ```go
  const appSettingEnrichmentRetroativo = "enrichment_retroativo"
  const retryDaysPadrao = 15
  const retryDaysMinimo = 1
  const retryDaysMaximo = 365

  type retroactiveEnrichmentSettings struct {
      RetryDays int `json:"retry_days"`
  }
  ```
  `defaultRetroactiveEnrichmentSettings()`, `obterRetroactiveEnrichmentSettings(ctx)`
  (mesmo formato de cache/leitura/fallback de `obterLanguageSettings`;
  reaproveita a MESMA struct `settingsCache` — acrescentar campos
  `retroCarregado bool` e `retro retroactiveEnrichmentSettings`, protegidos
  pelo `mu` já existente, sem criar um segundo mutex) e
  `(retroactiveEnrichmentSettings) valida() bool` (`RetryDays >= 1 &&
  RetryDays <= 365`). `getEnrichmentRetroativoSettings`/
  `putEnrichmentRetroativoSettings` espelham
  `getLanguageSettings`/`putLanguageSettings` linha a linha (`invalidarSettings`
  já zera `carregado`; acrescentar a limpeza de `retroCarregado` no mesmo
  método).
- **`internal/server/retroativo.go`**:
  - Remove a constante `intervaloReTentativaRetroativa` (30 dias fixos) e
    também `carimboRetroativoFormato` SE, ao ler o arquivo, ficar
    confirmado que não é usado por mais nada além do cálculo de `desde`
    (checar antes de apagar).
  - `EnriquecerRetroativo` passa a ler
    `cfgRetry, err := s.obterRetroactiveEnrichmentSettings(ctx)` e usa
    `cfgRetry.RetryDays` como o `?2` da query (ver mudança de SQL abaixo) —
    substitui o `desde` pré-formatado em Go por um parâmetro INTEIRO; quem
    calcula `datetime('now', '-N days')` passa a ser o SQLite, não mais o
    Go (evita duplicar a lógica de "dias → cutoff" em dois lugares quando o
    override por biblioteca entra em jogo).
  - Novo helper, no mesmo arquivo (NÃO reaproveita `idiomaDeBusca` —
    manter os dois resolvedores independentes; são pequenos e a fusão não
    compensa o acoplamento entre `settings.go`/`library`/idioma e
    retry-days):
    ```go
    // diasReentativaRetroativa resolve o intervalo de throttle aplicável a
    // uma entity: olha a biblioteca do PRIMEIRO media_file 'present' (mesmo
    // critério de desempate do idiomaDeBusca, 0084 — created_at ASC, id ASC);
    // com override definido, usa-o; sem override OU sem media_file algum,
    // cai no global. Ao contrário de idiomaDeBusca, ENTITY com media_files
    // de bibliotecas com overrides DIFERENTES usa o MENOR valor entre eles
    // (decisão proposital, ver §6) em vez de cair no global — aqui
    // ambiguidade só afeta QUANDO retentar, nunca o resultado da busca, então
    // errar para o lado de "retentar mais cedo" é seguro.
    func (s *Server) diasReentativaRetroativa(ctx context.Context, entityID string) (int, error)
    ```
    Usado só para DECISÃO informativa/testes unitários da regra de
    ambiguidade — a listagem em lote (abaixo) já resolve isso direto em
    SQL; este helper existe para o caso eventual de precisar expor "próxima
    tentativa prevista" no futuro (§7) sem duplicar a regra depois.
- **Query SQL** (`internal/db/queries/review.sql`,
  `ListarElegiveisEnriquecimentoRetroativo`): troca o parâmetro `sqlc.arg('desde')`
  (string pré-formatada) por `sqlc.arg('dias_global')` (inteiro) e resolve o
  cutoff POR LINHA via subquery correlacionada, usando o MENOR
  `retroactive_enrichment_days` entre as bibliotecas de origem da entity
  (ou o global, se nenhuma tiver override):
  ```sql
  -- name: ListarElegiveisEnriquecimentoRetroativo :many
  SELECT e.id, e.type, e.title, e.release_date, e.last_external_match_attempt_at
  FROM entity e
  WHERE e.external_ids_json IS NULL
    AND e.type IN ('movie', 'tv')
    AND e.status != 'draft'
    AND EXISTS (
          SELECT 1 FROM media_file m
          WHERE m.entity_id = e.id AND m.state = 'present'
        )
    AND NOT EXISTS (
          SELECT 1 FROM media_file m
          WHERE m.entity_id = e.id
            AND m.state = 'present'
            AND m.match_confidence > 0
            AND m.match_confidence < ?1
        )
    AND (
          e.last_external_match_attempt_at IS NULL
          OR e.last_external_match_attempt_at < COALESCE(
              (SELECT datetime('now', '-' || MIN(COALESCE(ls.retroactive_enrichment_days, ?2)) || ' days')
               FROM media_file mf
               JOIN library_source ls ON ls.id = mf.library_source_id
               WHERE mf.entity_id = e.id AND mf.state = 'present'),
              datetime('now', '-' || ?2 || ' days')
          )
        )
  ORDER BY
      (e.last_external_match_attempt_at IS NULL) DESC,
      e.last_external_match_attempt_at ASC,
      e.title COLLATE NOCASE ASC,
      e.id
  LIMIT ?3
  ```
  `Limiar` (?1) e `Limite` (?3) continuam iguais a hoje; `?2` passa a ser
  `GlobalDias int64`, não mais uma string `desde`. Regenerar com
  `sqlc generate`.
- **`internal/server/library.go`**: `createRequest`/`patchSourceRequest`/
  `sourceDTO` ganham `RetroactiveEnrichmentDays` (ponteiro; no PATCH,
  aplicar a MESMA correção de nullable já documentada no 0084 — não usar
  ponteiro direto num campo que precisa distinguir "ausente" de "null" via
  `encoding/json`, testar explicitamente com um teste equivalente a
  `TestPatchLibraryPreferredLanguageDefineELimpa`); `createLibrary`/
  `patchLibrary` validam `1 <= v <= 365` antes de gravar (`422
  invalid_body`).
- **`internal/server/server.go`**: registrar as duas novas rotas
  (`GET`/`PUT /api/v1/settings/enrichment-retroativo`), mesmo grupo de
  `/settings/languages`.

### Frontend (Vite/React)
- `web/src/routes/settings.tsx`: nova seção/card "Enriquecimento
  retroativo" com um `Input type="number"` (min 1, max 365) "Repetir
  tentativa após (dias)", carregado de `GET /settings/enrichment-retroativo`
  e salvo com `PUT` no mesmo padrão (TanStack Form + Zod + Query, igual ao
  bloco de idiomas já existente na mesma tela).
- `web/src/routes/library-new.tsx` / `library-edit.tsx`: novo campo
  numérico opcional "Reentativa de enriquecimento (dias)", ao lado do
  select de idioma preferido (0084); placeholder interpola o valor global
  atual (busca `enrichment-retroativo` junto dos outros dados do
  formulário); vazio → `null` no PATCH/POST (herda o global).
- `web/src/lib/api-types.ts`: regenerar via `npm run gen:types`.

### Arquivos afetados
- `internal/db/migrations/0018_library_source_dias_reentativa.sql` (novo)
- `internal/db/queries/library_source.sql`
- `internal/db/queries/review.sql`
- `internal/library/library.go`
- `internal/server/settings.go`
- `internal/server/retroativo.go`
- `internal/server/library.go`
- `internal/server/server.go` (rotas)
- `openapi.yaml`
- `web/src/routes/settings.tsx`
- `web/src/routes/library-new.tsx`
- `web/src/routes/library-edit.tsx`
- `web/src/lib/api-types.ts` (gerado)

## 5. Critérios de aceite

- [ ] `GET /settings/enrichment-retroativo` sem configuração prévia
      devolve `{"retry_days": 15}`.
- [ ] `PUT` com `retry_days: 7` persiste; `GET` subsequente reflete 7.
- [ ] `PUT` com `retry_days: 0` ou `retry_days: 400` → `422 invalid_body`;
      valor anterior preservado.
- [ ] `POST /library` com `retroactive_enrichment_days` fora de `[1, 365]`
      → `422 invalid_body`.
- [ ] `PATCH /library/{id}` com `retroactive_enrichment_days: 3` persiste;
      `null` explícito limpa (volta a herdar o global); campo ausente não
      altera o valor atual.
- [ ] Item cujo ÚNICO `media_file` pertence a uma biblioteca com override
      de 3 dias E `last_external_match_attempt_at` há 4 dias: aparece como
      elegível na próxima execução do job (retentado antes do global).
- [ ] Item de biblioteca SEM override, com `last_external_match_attempt_at`
      há menos dias que o global configurado: NÃO aparece como elegível.
- [ ] Entity com `media_file` de duas bibliotecas com overrides diferentes
      (ex.: 3 e 10 dias): elegível a partir de 3 dias (usa o MENOR).
- [ ] Trocar o valor global via `PUT` muda o comportamento do job na
      PRÓXIMA execução, sem reiniciar o servidor (cache invalidado no PUT).
- [ ] `limiteEnriquecimentoRetroativoPorExecucao` (teto de 15
      itens/execução) e `limiarSinalizacaoRetroativa` (50.0) continuam
      exatamente como antes — nenhum teste existente desses dois quebra.
- [ ] `POST /entities/{id}/enrich-missing` continua funcionando sem
      nenhuma guarda de throttle (comportamento do 0056 §6, inalterado).
- [ ] UI: `/dashboard/settings` mostra e salva o campo global; criar/editar
      biblioteca mostra e salva o override com o placeholder do valor
      global efetivo.

## 6. Notas de implementação / decisões

- **Por que `app_settings` e não `job_definition.config_json`**: o
  mecanismo de `config_json` namespaced já existe (usado pelo job de
  backup, `{"keep": N}`), mas HOJE não há nenhuma superfície HTTP para
  editá-lo (`PATCH /jobs/{id}` só toca `enabled`/`schedule_cron`,
  `internal/server/jobs.go`). Estender esse endpoint para aceitar
  `config_json` genérico é uma mudança de escopo maior e não pedida pelo
  dono; `app_settings` já tem o padrão completo (cache, GET/PUT, validação,
  invalidação) pronto e testado desde o 0019 — reaproveitar é
  estritamente mais simples que abrir uma segunda superfície de config.
- **Por que a regra de ambiguidade é "menor valor" aqui, mas "cai pro
  global" no idioma (0084)**: para idioma, escolher errado ativamente
  piora a busca (idioma errado = candidato errado). Para o throttle,
  ambiguidade só decide QUANDO retentar — usar o menor valor entre as
  bibliotecas envolvidas nunca causa dano (na pior hipótese, uma tentativa
  a mais um pouco antes do que seria "ideal"), e evita que uma entity rara
  (arquivos de duas bibliotecas) fique presa no intervalo mais longo por
  acidente.
- **Por que resolver o cutoff em SQL (subquery correlacionada) em vez de
  Go**: evita duplicar "converter dias → timestamp de corte" em dois
  lugares (Go E SQL) e mantém a listagem elegível como UMA query só,
  sem paginação/filtragem em duas camadas. O helper Go
  `diasReentativaRetroativa` continua existindo separadamente, não para a
  listagem, mas para eventual uso futuro (§7) sem duplicar a regra de
  ambiguidade depois.
- **Por que o default muda de 30 para 15 dias**: pedido explícito do dono
  nesta sessão, motivado pelo caso real de 3 itens presos por semanas por
  causa de uma falha transiente do provider/mount que o throttle de 30
  dias não deixava recuperar rápido o bastante.
- **Sem `CHECK` no SQLite para `retroactive_enrichment_days`**: mesmo
  raciocínio de `preferred_language` — mesmo sendo um intervalo numérico
  fixo (ao contrário do idioma, que é dinâmico), manter a MESMA convenção
  do projeto (validação só na camada de aplicação) evita duas fontes de
  verdade para a mesma regra (1–365) — a validação em Go já é suficiente e
  testável.

## 7. Fora deste briefing / follow-ups

- Tela de "próxima tentativa prevista" por item (usaria
  `diasReentativaRetroativa` + `last_external_match_attempt_at` para
  mostrar uma data ao dono) — não pedido, fica para depois se houver
  demanda.
- Expor `config_json` de jobs via `PATCH /jobs/{id}` como mecanismo
  genérico de tuning — avaliar como decisão própria se surgirem mais casos
  como este (hoje só o retry-days motivou a discussão).
- Configurar `limiteEnriquecimentoRetroativoPorExecucao`/
  `limiarSinalizacaoRetroativa` também via painel — fora de escopo aqui,
  não foi pedido.
