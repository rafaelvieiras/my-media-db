# 0120 — Reconciliação de filhos órfãos (faixa/episódio) contra árvore já identificada

> Um arquivo escaneado nunca deveria ficar preso num rascunho para sempre só
> porque o match falhou na hora certa uma vez. Fecha o buraco: um passo novo
> no `saude_biblioteca` retenta religar faixa/episódio órfão contra a árvore
> pai (artista/álbum, série) sempre que ela existir ou vier a existir depois
> — e corrige um bug ativo de série que hoje BLOQUEIA esse retry pra sempre.

| | |
|---|---|
| **ID** | 0120 |
| **Tipo** | fix/feature |
| **Fase (roadmap)** | Transversal / infraestrutura operacional — fecha o gap descoberto ao investigar o 0119 em produção (Bomfunk MC's). |
| **Docs relacionadas** | [docs/00-agenda-de-decisoes.md](../docs/00-agenda-de-decisoes.md) (MD1/MD5), [ADR-0021](../docs/adr/0021-modelo-de-series-hierarquia-ordenacoes-vinculo-arquivo.md), [ADR-0027](../docs/adr/0027-modelo-de-musica-hierarquia-artista-album-faixa.md) |
| **Status** | done |
| **Depende de** | [0034](0034-scanner-de-series-arquivo-episodio.md), [0056](0056-enriquecimento-externo-retroativo-job-saude.md), [0107](0107-scanner-matching-arquivo-faixa.md), [0115](0115-musica-import-local-first.md), [0116](0116-enriquecimento-retroativo-musica-artista.md), [0118](0118-reprocessar-artes-faltantes-entities-ja-identificadas.md) |

---

## 1. Objetivo

Ao investigar por que as faixas do "Bomfunk MC's" apareciam em revisão
(pergunta do dono), achamos que **não é bug de matching, é ausência de
reconciliação**: o pipeline de match (`resolverFaixa`/`matchTextual` em
`internal/scanner/musica.go`, `tvmatch.Casar` em `internal/scanner/serie.go`)
roda **uma única vez**, no momento do scan daquele arquivo. Se não resolveu
com confiança ali, o resultado (`draft`, ou vínculo nenhum) fica **para
sempre** — nada no sistema tenta de novo depois, mesmo quando a árvore
pai (artista/álbum, série) passa a existir com identidade completa.

O dono perguntou se isso vale pra qualquer tipo de arquivo. A resposta,
investigada no código:

- **Filme**: não sofre disso. 1 arquivo = 1 entity, sem nível pai/filho
  intermediário — `EnriquecerRetroativo`/auto-match (0056) já cobre "entity
  que precisa de identidade" porque a entity do arquivo É a obra.
- **Música**: sofre (caso confirmado: artista+álbum "Freestyler" existem com
  `mbid` e `cataloged`, zero faixas vinculadas; a faixa real do disco existe
  separada, `draft`, sem `ancestors`).
- **Série**: sofre do MESMO jeito estruturalmente — E tem um bug ATIVO mais
  grave: `acharSeriePorTitulo` (`internal/scanner/serie.go:237-246`) acha
  qualquer entity `type=tv` pelo título **sem filtrar `status`**, e essa
  checagem roda **antes** da tentativa de auto-import
  (`resolverSerieMiss`, serie.go:169 vem antes de serie.go:173). Ou seja:
  uma vez que uma série vira rascunho de revisão, todo scan futuro dessa
  pasta reencontra o MESMO rascunho e **nunca mais tenta importar de
  verdade** — não é "falta retry", é "ativamente impede retry".

`docs/00-agenda-de-decisoes.md` (MD5) já define "órfã" como *arquivo que
sumiu do disco* — um conceito ortogonal a este gap (aqui o arquivo está
presente e catalogado; o problema é vínculo/identidade nunca reconciliados).

## 2. Jornada do usuário

1. Um arquivo de música/série é escaneado antes de a árvore pai (artista/
   álbum, ou série) ter identidade resolvida — ou o match textual falha por
   qualquer razão pontual (rede, ambiguidade momentânea). O arquivo fica
   catalogado (posse=sim) mas em rascunho/sem vínculo — aparece na fila de
   revisão.
2. Depois, a árvore pai passa a existir (import manual do dono, enriquecimento
   retroativo do artista, ou uma segunda tentativa de import de série).
3. No próximo ciclo do `saude_biblioteca`, um passo novo tenta **de novo** o
   match daquele arquivo específico contra a árvore pai já identificada — sem
   o dono precisar mexer em nada. Sucesso: a faixa/episódio some da fila de
   revisão, aparece linkado no lugar certo. Falha: continua em revisão,
   tentando de novo no próximo ciclo (mesmo espírito de throttle do 0118).

## 3. Escopo

**Dentro:**

1. **Fix do bug de série** (`acharSeriePorTitulo`): passa a IGNORAR entities
   `type=tv` com `status='draft'` — uma série-rascunho não deve mais ser
   reencontrada como se fosse identidade resolvida; o fluxo cai para a
   tentativa de auto-import (`ImportarSeriePorNome`) normalmente. Sem esse
   fix, a reconciliação de série (item 3 abaixo) reimportaria a série e no
   PRÓXIMO scan voltaria a achar o rascunho antigo pelo título — o fix é
   pré-requisito, não just-in-case.
2. **Refatoração mínima e sem mudança de comportamento** pra expor a lógica
   de match como funções reutilizáveis fora do `scanRun`:
   - `internal/scanner/musica.go`: extrair o corpo de `matchTextual` para um
     método `(*Manager).matchTextualFaixa(ctx, tags, langs) (string, bool)`
     usando `m.musicaProvider`/`m.musicaImportador` diretamente; `scanRun.
     matchTextual` vira um repasse de uma linha.
   - `internal/scanner/serie.go`: extrair o branch de auto-import (serie.go:
     173-192, SEM o find-by-título que fica só no caminho de scan) para um
     método `(*Manager).tentarImportarSerie(ctx, nome, langs, preferredLang)
     (serieResolvida, bool)`.
3. **`Manager.ReconciliarFaixaDraft(ctx, entityID string) (linked bool, err
   error)`** (novo, `internal/scanner/reconciliar.go`): dado o id de uma
   faixa `draft` órfã (sem `entity_relation` como filha), lê seu
   `media_file` (path), re-extrai tags via `m.prober.Probe` (mesmo Prober já
   injetado no Manager) e chama `m.matchTextualFaixa`. Se resolver: repointa
   o `media_file.entity_id` (novo `AtualizarMediaFileEntityID`, ver §4) da
   faixa-rascunho pra faixa recém-resolvida, com `match_confidence` do match
   textual. A faixa-rascunho fica sem nenhum `media_file` (posse cai pra
   não) — some da fila de revisão pelo mecanismo JÁ existente (posse
   derivada), sem apagar nada (princípio 5).
4. **`Manager.ReconciliarSerieDraft(ctx, entityID string) (linked bool, err
   error)`** (mesmo arquivo): dado o id de uma série `draft`, tenta
   `m.tentarImportarSerie` de novo com o título da entity. Se resolver: para
   cada `media_file` hoje apontando pra essa série-rascunho, re-deriva o
   episódio (`naming.ParseEpisode` no basename do path) e casa
   (`tvmatch.Casar`) contra o índice da série recém-importada; repointa
   `media_file.entity_id` pra série real e cria os vínculos
   `media_file_entity` (mesmo `CriarVinculoEntidade` já usado em
   `gravarArquivoSerie`). Série-rascunho some da fila do mesmo jeito (posse
   derivada cai a zero).
5. **`Server.ReconciliarFilhosOrfaos(ctx) (reconciliados int, err error)`**
   (`internal/server/reconciliar_orfaos.go`, novo): lista candidatos —
   faixas `draft` órfãs (`ListarFaixasDraftOrfas`, nova query) e séries
   `draft`/`em revisão` com arquivo (`ListarSeriesDraftComArquivo`, nova
   query) —, throttlado pelo MESMO `RetroactiveEnrichmentSettings` do
   0118/0116 (reusa `obterRetroactiveEnrichmentSettings`, mesmo
   `last_external_match_attempt_at` como carimbo — terceiro uso do campo
   pra um universo disjunto: faixa/série draft nunca tem esse campo usado
   pelos outros dois usos, 0117/0118 são só entities JÁ identificadas).
   Chama `s.scanner.ReconciliarFaixaDraft`/`ReconciliarSerieDraft` conforme o
   tipo, carimba a tentativa em qualquer caso (sucesso ou não, evita
   martelar o mesmo item todo ciclo).
6. Novo passo no job `saude_biblioteca`, depois de `DescobrirArteLocalMusica`
   (0119): `Matcher.ReconciliarFilhosOrfaos`; cláusula na mensagem final "N
   faixa(s)/episódio(s) religados à árvore" só quando N > 0.

**Fora (explicitamente):**
- Filme: não entra, não sofre do gap (§1).
- Merge/fusão de duas entities JÁ ambas identificadas com o mesmo mbid/id
  externo (isso é o fluxo de colisão externa já existente, `internal/server/
  duplicata.go`) — aqui uma das duas pontas (a "rascunho órfã") nunca teve
  identidade própria, não é uma fusão de duplicatas.
- Corrigir a causa de o match textual/de série falhar na hora do scan (rede,
  ambiguidade pontual) — fora de escopo, é o próprio motivo de a
  reconciliação existir como rede de segurança.
- UI dedicada para acompanhar a reconciliação — a fila de revisão existente
  já reflete o resultado (item some quando religado).

## 4. Contrato técnico

### Dados / modelo
- Nenhuma migração nova.

### Backend (Go)
- `internal/db/queries/media_file.sql` (+ sqlc):
  - `AtualizarMediaFileEntityID :exec` — `UPDATE media_file SET entity_id =
    ?, match_confidence = ?, updated_at = strftime(...) WHERE id = ?`.
- `internal/db/queries/review.sql` (ou entity.sql, a definir na
  implementação) (+ sqlc):
  - `ListarFaixasDraftOrfas :many` — `type='faixa'`, `status='draft'`, existe
    `media_file` `state='present'` com `entity_id = faixa.id`, e NÃO existe
    `entity_relation` com `child_id = faixa.id`.
  - `ListarSeriesDraftComArquivo :many` — `type='tv'`, `status='draft'`,
    existe `media_file` `state='present'` com `entity_id = serie.id`.
- `internal/scanner/musica.go`: `matchTextual` refatorado (repasse), lógica
  movida pra `(*Manager).matchTextualFaixa`.
- `internal/scanner/serie.go`: `acharSeriePorTitulo` ganha filtro de status;
  branch de auto-import extraído pra `(*Manager).tentarImportarSerie`.
- `internal/scanner/reconciliar.go` (novo): `Manager.ReconciliarFaixaDraft`,
  `Manager.ReconciliarSerieDraft`.
- `internal/server/reconciliar_orfaos.go` (novo): `Server.
  ReconciliarFilhosOrfaos`.
- `internal/job/libraryhealth/libraryhealth.go`: `Matcher.
  ReconciliarFilhosOrfaos`, `rodarReconciliacaoFilhosOrfaos`, wiring em
  `Executar` (depois de `rodarDescobertaArteLocalMusica`), cláusula nova em
  `montarMensagem`.

### Arquivos afetados
- `internal/scanner/musica.go`, `internal/scanner/serie.go`,
  `internal/scanner/reconciliar.go` (+ `_test.go`).
- `internal/db/queries/media_file.sql`, `internal/db/queries/review.sql` (ou
  `entity.sql`), `internal/db/sqlcgen/*.go` (gerado).
- `internal/server/reconciliar_orfaos.go` (+ `_test.go`).
- `internal/job/libraryhealth/libraryhealth.go` (+ `_test.go`).
- `internal/db/sqlcgen/crud_test.go` (cobertura direta das queries novas).

## 5. Critérios de aceite

- [x] Uma série que virou rascunho e depois é re-tentada encontra
      `ImportarSeriePorNome` de novo (não fica presa em
      `acharSeriePorTitulo` para sempre) — teste cobrindo o fix isolado
      (`internal/scanner/serie_test.go`) e provado ponta-a-ponta via HTTP em
      `TestJornadaReconciliacaoAutomaticaSerieDraft`
      (`internal/e2e/journeys_reconciliar_test.go`).
- [x] Uma faixa `draft` órfã com arquivo presente, quando o artista/álbum já
      têm identidade (ou passam a ter via provider), é religada: some da
      revisão, aparece com `ancestors` preenchido
      (`internal/scanner/reconciliar_test.go`,
      `internal/server/reconciliar_orfaos_test.go`).
- [x] Uma série `draft` com episódios órfãos, quando a série é reimportada
      com sucesso, tem seus arquivos repontados pra série real com os
      vínculos de episódio corretos.
- [x] Sem candidato nenhum (nenhuma faixa/série draft-com-arquivo): passo
      roda sem erro, sem cláusula na mensagem.
- [x] Throttle: mesmo item não é retentado antes de `RetryDays`
      (`obterRetroactiveEnrichmentSettings`, igual 0116/0118).
- [x] `go build ./...`, `go vet ./...`, `go test ./... -count=1` verdes;
      `make gate-local` sem regressão de cobertura (cobertura de linhas
      75.8%→75.9%, cobertura E2E de jornadas 42.1%→46.4%).

## 6. Notas de implementação / decisões

- O fix de `acharSeriePorTitulo` é o único ponto deste briefing que muda
  comportamento do SCAN em si (não só do job de reconciliação) — é
  deliberado: sem ele, a reconciliação de série readquire e IMEDIATAMENTE
  perderia a identidade de novo no próximo scan (voltaria a achar o
  rascunho antigo pelo título antes de checar o id externo/import).
- `last_external_match_attempt_at` passa a ter um QUARTO uso semântico
  disjunto (retry de match/tentativa de artista sem match / arte faltante
  de entity identificada / agora reconciliação de filho draft) — mesmo
  padrão já estabelecido no 0118, universos sempre mutuamente exclusivos por
  `(type, status, external_ids/ancestors)`, então não há colisão de
  semântica dentro da mesma linha.
- Reconciliação de música não duplica `matchTextual`: extrai a lógica pra
  `Manager` (que já tem `musicaProvider`/`musicaImportador`/`prober`),
  evitando dois lugares com a mesma regra de matching (risco real de
  divergirem com o tempo).

## 7. Fora deste briefing / follow-ups

- Se o dono quiser visibilidade explícita de "isso foi religado
  automaticamente" (histórico/log de reconciliação na UI), fica como
  evolução futura — hoje o item só some da revisão e aparece linkado, sem
  rastro visível de que passou por reconciliação.
- **Gap descoberto ao validar (E2E) — rascunho vazio nunca sai da fila de
  revisão**: `ReconciliarSerieDraft`/`ReconciliarFaixaDraft` são
  deliberadamente não-destrutivos (princípio 5): eles repontam o
  `media_file` pra entity real e NUNCA tocam o `status`/existência do
  rascunho original. Como `ListarRevisao` lista todo `status='draft'`
  incondicionalmente (sem olhar posse), o rascunho agora vazio (`owned=
  false`, zero arquivo) continua aparecendo em `/review` para sempre — só
  que sem mais nenhum arquivo preso (o problema real, arquivo grudado numa
  entity errada, está mesmo resolvido; o que sobra é só um registro vazio
  esperando o dono apagar/mesclar manualmente se quiser, ou uma fila de
  revisão que passa a listar itens "mortos"). Corrigir isso (ex.: excluir de
  `/review` um `draft` sem posse e sem ter sido criado há pouco tempo, ou
  achar/reusar um mecanismo de purga) fica para um briefing seguinte — fora
  do escopo desta rede de segurança de match.
