# 0088 — Três ações forçadas no menu Gerenciar: arquivos, metadados, artes

> O dono já tem "Baixar informações faltando"/"Baixar arte faltando" no menu
> Gerenciar, mas os dois só aparecem quando `catalog_completeness` acusa uma
> lacuna e nenhum dos dois ignora o cache/debounce nem revarre o disco. Este
> briefing soma 3 ações sempre visíveis e incondicionais: reconferir os
> arquivos/assets/campos locais de reprodução em disco, forçar um novo
> download de metadados pulando o cache de 24h, e forçar uma nova consulta ao
> fanart.tv seguida da materialização do que estiver faltando na pasta real.

| | |
|---|---|
| **ID** | 0088 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Status** | done |
| **Docs relacionadas** | [07-enriquecimento-providers](../docs/07-enriquecimento-providers.md), [08-artwork-e-assets](../docs/08-artwork-e-assets.md) |
| **Depende de** | 0048, 0056, 0062, 0064, 0086, 0087 |

---

## 1. Objetivo

Hoje, fora do scan/watch e do enriquecimento automático (com seu throttle de
dias, 0085), o dono não tem como pedir explicitamente "confere de novo" em
três frentes distintas:

1. **Arquivos/assets em disco** — o scanner só re-hasheia quando mtime/size
   mudam (incremental, intencional — não mexer nisso) e só importa um
   `poster.jpg`/asset local colocado manualmente na pasta **na primeira
   varredura daquela pasta**; se o dono adicionar/trocar esse arquivo depois
   (entity já catalogada), ele nunca é pego — nem por scan incremental nem
   por scan completo repetido. Investigado no código
   (`internal/scanner/match.go`, `criarEntity`): `CriarAsset(kind="poster",
   origin="local")` só roda no branch "entity não existia ainda".
2. **Metadados** — `POST /entities/{id}/enrich` e afins já ignoram o throttle
   de dias do job automático, mas todos passam por
   `internal/externaldetail.Servico.PayloadFresco`, cache por
   `(source, source_id)` com TTL de 24h — sem bypass hoje. Se o provider
   corrigiu um dado errado na fonte há poucas horas, o dono não consegue
   forçar a atualização sem esperar o cache expirar.
3. **Artes faltando** — `s.MaterializarMidiasFaltantes` (usada pelo job
   `midias_faltantes`) já existe por entity, mas só materializa o candidato
   **já resolvido** no inventário; ela não dispara uma nova consulta ao
   fanart.tv. O inventário (`media_external(source='fanarttv')`) só é escrito
   por `enriquecerArtes`, hoje chamado automaticamente nos fluxos de
   import/enriquecimento (0087) — não há como o dono pedir "vai lá de novo no
   fanart.tv agora" sem re-disparar todo o enriquecimento de metadados.

Além disso, o dono quer que essa mesma "reconferida de arquivos" também
resgate um conjunto específico de **campos locais de reprodução** — mais
vistos, avaliação do usuário, assistido, última reprodução — quando eles
existem no `.nfo` já presente na pasta (escrito por nós ou por outro software
como Kodi/Jellyfin/Emby apontando pra mesma pasta) mas ainda **faltam no
banco**. Isso é uma extensão pontual e explicitamente escopada do princípio
"NFO é exportação one-way" (`AGENTS.md`/briefing 0062): continua não
existindo releitura de metadados de catálogo (título, overview, elenco...) a
partir do `.nfo` — só esse conjunto pequeno de campos "estado local de
reprodução", que nunca são escritos por nenhum provider externo, só por quem
manuseia o arquivo (o próprio dono, ou outro software local), e só quando o
banco ainda não tem valor (preenche lacuna, nunca sobrescreve — coerente com
princípio 1 "override do dono vence" e princípio 5 "não-destrutivo": se já
temos um valor, ele já É a fonte de verdade local, não se sobrescreve com o
que está no arquivo).

## 2. Jornada do usuário

1. O dono abre o detalhe de uma entity (filme, série, temporada ou episódio)
   e clica em "Gerenciar".
2. Abaixo dos itens já existentes, um separador introduz um grupo novo com 3
   ações sempre visíveis (não dependem de `catalog_completeness`):
   - **"Verificar Atualizações de Arquivos"** — força reconferir os arquivos
     físicos da entity (re-hash mesmo sem mudança de mtime/size), procura
     assets locais novos/atualizados na(s) pasta(s) e, se houver `.nfo` no
     destino, preenche no banco os campos locais de reprodução que ainda
     estiverem vazios.
   - **"Baixar Metadados"** — repete o enriquecimento de metadados da entity
     ignorando o cache de 24h (sempre vai à rede buscar o detalhe mais
     recente do provider).
   - **"Baixar Artes Faltantes"** — consulta de novo o(s) `ArteProvider`
     registrado(s) (hoje: fanart.tv) para atualizar o inventário de artes da
     entity e, na sequência, materializa na pasta real o que estiver
     faltando (mesmo efeito do job `midias_faltantes`, só que agora
     imediato e sob demanda, e só para o item corrente).
3. Cada ação roda de forma assíncrona simples (um `POST`, resposta quando
   terminar) com feedback de "processando…"/sucesso/erro no mesmo padrão dos
   itens que já existem no menu.
4. Ao terminar com sucesso, os dados relevantes na tela (arquivos, galeria de
   artes, campos do detalhe) refletem o resultado sem precisar de F5 —
   invalidação de cache do TanStack Query, mesmo padrão já usado pelos itens
   existentes do menu.

## 3. Escopo

**Dentro:**
- Novas colunas em `entity` para os campos locais de reprodução (ver §4) +
  migration.
- Nova capacidade no `internal/scanner` para reconferir (force re-hash +
  mediainfo) só os arquivos de UMA entity, sem varrer o datasource inteiro.
- Extensão do fluxo de descoberta de asset local para rodar também quando a
  entity **já existe** (hoje só roda na criação) — sem mudar precedência
  (`escolherAssetServido`), só garantindo que o asset `origin='local'` exista
  e aponte pro arquivo certo.
- Leitura pontual do `.nfo` já em disco (`entity.nfo_path`), reaproveitando
  `internal/localmedia.ExtrairCamposUsuario` (já existe, hoje só usado
  efemeramente dentro da regravação do NFO) para preencher, **só se
  ausente no banco**, os campos: assistido, contagem de reproduções,
  avaliação do usuário, última reprodução, posição/total de retomada.
- Parâmetro `force` propagado por `externaldetail.PayloadFresco` →
  `enrich.Servico.Enriquecer` → handler `POST /entities/{id}/enrich`, para
  pular o TTL de 24h sob pedido explícito.
- Wrapper público em `internal/enrich` para expor `enriquecerArtes` sem
  precisar rodar todo `Enriquecer`, seguido da chamada já existente a
  `s.MaterializarMidiasFaltantes`.
- 2 endpoints novos (`refresh-files`, `materialize-missing-media`) + extensão
  de body do endpoint já existente (`enrich` ganha `force`).
- 3 itens novos no menu "Gerenciar" (sempre visíveis, com separador), mesmo
  padrão de `useMutation` + feedback já usado pelos itens existentes.
- Testes de regressão (Go: unit; frontend: o componente já tem suíte própria
  — estender).

**Fora (explicitamente):**
- Reler o `.nfo` para qualquer campo de CATÁLOGO (título, overview, elenco,
  gênero...) — isso continua vindo só do provider/override, nunca do `.nfo`.
  Os campos locais de reprodução são a ÚNICA exceção, e são aditivos
  (preenche vazio, nunca sobrescreve).
- Cascatear "Baixar Artes Faltantes"/"Verificar Atualizações de Arquivos"
  para os filhos de uma série (temporadas/episódios) automaticamente — a
  ação vale só para a entity aberta na tela, igual às demais ações do menu
  hoje. Se o dono quiser repetir em cascata, repete manualmente ou espera o
  job periódico.
- Editar campos locais de reprodução pela UI (marcar como assistido,
  zerar contagem etc.) — isso é feature própria (tipo "watch status
  manual"), fora de escopo aqui; este briefing só cobre a IMPORTAÇÃO
  passiva a partir do `.nfo` existente.
- Mudar o comportamento do job automático `midias_faltantes` ou do
  enriquecimento retroativo (0085) — as 3 ações são gatilhos manuais e
  pontuais, paralelos aos jobs, não uma mudança neles.
- Qualquer UI nova para mostrar os campos locais de reprodução no detalhe da
  entity (nota do usuário, "assistido" etc.) — persistimos o dado; exibir é
  outro briefing se o dono quiser.

## 4. Contrato técnico

### Dados / modelo

Migration nova (`internal/db/migrations/0019_campos_locais_reproducao.sql`),
`ALTER TABLE entity ADD COLUMN`:

```sql
ALTER TABLE entity ADD COLUMN watched INTEGER CHECK (watched IN (0, 1));
ALTER TABLE entity ADD COLUMN play_count INTEGER;
ALTER TABLE entity ADD COLUMN user_rating REAL;
ALTER TABLE entity ADD COLUMN last_played_at TEXT;
ALTER TABLE entity ADD COLUMN resume_position_seconds REAL;
ALTER TABLE entity ADD COLUMN resume_total_seconds REAL;
```

Todas nullable, sem default — ausência = "nunca lido do `.nfo`, nunca setado
por ninguém". Não passam pelo resolver de 3 camadas (não são campo de
catálogo, não têm noção de idioma/fonte externa) — são direto na `entity`,
mesmo tratamento de `local_path`/`nfo_path` (briefing 0062).

Nova query sqlc (`internal/db/queries/entity.sql`), UPDATE que só preenche o
que estiver `NULL` (idempotente, sem race — single-writer WAL, ADR-0010):

```sql
-- name: PreencherCamposLocaisReproducaoSeAusentes :exec
UPDATE entity SET
    watched = COALESCE(watched, ?),
    play_count = COALESCE(play_count, ?),
    user_rating = COALESCE(user_rating, ?),
    last_played_at = COALESCE(last_played_at, ?),
    resume_position_seconds = COALESCE(resume_position_seconds, ?),
    resume_total_seconds = COALESCE(resume_total_seconds, ?)
WHERE id = ?;
```

`ObterEntity` (e qualquer outra query que já faça `SELECT *`/lista explícita
de colunas da `entity`) precisa regenerar via `sqlc generate` para expor os
campos novos — conferir se alguma query lista colunas explicitamente e
precisa de edição manual antes de gerar.

### API

- `POST /api/v1/entities/{id}/refresh-files` — sem body. Resposta 200 com um
  resumo (ex.: `{"files_rechecked": N, "assets_found": N,
  "playback_fields_filled": ["watched", "play_count"]}` — moldar conforme o
  que a implementação naturalmente produzir; não precisa ser rígido, é só
  feedback informativo pro dono). Erros: 404 se entity não existe.
- `POST /api/v1/entities/{id}/enrich` — **estender** o body já existente
  (hoje aceita opcionalmente trocar `source`/`source_id`) com um campo
  opcional `force: boolean` (default `false`). Quando `true`, propaga até
  `PayloadFresco` para pular o TTL de 24h. Sem mudança de resposta.
- `POST /api/v1/entities/{id}/materialize-missing-media` — sem body.
  Internamente: (1) chama o wrapper público de `enriquecerArtes` para
  atualizar o inventário de artes da entity; (2) chama
  `s.MaterializarMidiasFaltantes(ctx, id)` (já existe, reaproveitar sem
  alterar). Resposta 200 com o mesmo formato que
  `MaterializarMidiasFaltantes` já devolve hoje (`artes_baixadas`,
  `nfo_gravado` — conferir o shape usado pelo job/handler existente e
  reaproveitar).
- Atualizar `openapi.yaml` com os 2 paths novos e o campo `force` no
  request body de `/entities/{id}/enrich`.

### Backend (Go)

- `internal/scanner`: nova função pública no `Manager` (nome sugerido
  `RecheckEntityFiles` ou similar) que recebe os paths + `library.Source` de
  cada arquivo (resolvidos pelo chamador via `ListarMediaFilesPorEntity`,
  mesmo padrão já usado em `internal/server/retroativo.go` para achar a
  `library_source` de uma entity) e força o re-hash/mediainfo **daqueles
  paths específicos**, reaproveitando as funções internas já existentes
  (`computarFacts`/`hashEMediainfo`/`gravarArquivo` ou equivalentes) só que
  pulando o short-circuit de `mediaFileInalteradoPorPath` — ou seja, sempre
  re-hasheia os paths passados, mesmo se mtime/size baterem. **Não** mexe no
  comportamento do scan de datasource inteiro (`Start`), que continua
  incremental como está — isso é aditivo, um caminho de entrada novo.
- Descoberta de asset local em entity já existente: no ponto em que o scan
  descobre `f.poster`/equivalente para um arquivo cujo `resolverEntity` bate
  numa entity JÁ existente (branches de `internal/scanner/match.go` fora de
  `criarEntity`) — ou, mais simples e mais isolado do scan completo, uma
  função nova chamada só pelo endpoint `refresh-files` que reaproveita a
  mesma lógica de descoberta de imagem já usada por `descobrirPoster` sobre
  a pasta de cada `media_file` da entity, e faz upsert do asset
  `origin='local'` (cria se não existir; atualiza `path_or_uri` se o arquivo
  encontrado mudou). Preferir esta segunda opção — mais isolada, sem tocar
  no fluxo de scan de datasource inteiro. Para série: se `descobrirPoster`
  equivalente não existir hoje em `internal/scanner/serie.go` (confirmado
  na investigação — o fluxo de série não chama `descobrirPoster`), a nova
  função de `refresh-files` deve buscar direto na pasta do(s) `media_file`
  da entity, sem depender do fluxo de scan de filme.
- Campos locais de reprodução: dentro do mesmo handler/fluxo de
  `refresh-files`, se `entity.nfo_path` estiver setado e o arquivo existir
  em disco, ler com `internal/localmedia.ExtrairCamposUsuario` (já existe) e
  chamar `PreencherCamposLocaisReproducaoSeAusentes` com os valores lidos
  (passar `NULL`/zero-value para o que o `.nfo` não trouxer — o `COALESCE`
  cuida de não sobrescrever).
- `internal/externaldetail/externaldetail.go`: `PayloadFresco` ganha
  parâmetro `force bool` — quando `true`, pula a checagem de
  `doCacheFresco` e vai direto ao provider, mas continua persistindo o
  resultado fresco no cache (mesmo `persistir` de hoje).
- `internal/enrich/enrich.go`: `Enriquecer` ganha parâmetro `force bool`,
  propagado internamente até as chamadas de `PayloadFresco` dentro de
  `enriquecerBase` (base + enriquecimento rico). Atualizar TODOS os
  call sites de `Enriquecer` (server: `enrich.go`, `review.go`
  [Corrigir/Confirmar candidato], `automatch.go`, `retroativo.go`) passando
  `false`, exceto o novo caminho explícito de "Baixar Metadados" que passa
  `true`.
- `internal/enrich/artes.go`: expor um wrapper público (ex.
  `func (s *Servico) AtualizarInventarioArtes(ctx context.Context, entityID,
  tipo string, langs []string) error`) que só chama a `enriquecerArtes` já
  existente — sem duplicar lógica.
- Handlers novos em `internal/server` (arquivo novo, ex.
  `refresh_files.go`, ou anexar em `midias_faltantes.go` conforme fizer
  mais sentido para quem implementar) registrados em `server.go` junto das
  demais rotas de `/entities/{id}/...`.

### Frontend (Vite/React)

- `web/src/lib/api.ts`: 2 funções novas (`refreshEntityFiles(id)`,
  `materializeMissingMedia(id)`) seguindo o padrão de `downloadMissingArtwork`
  já existente; estender a função de enrich já existente para aceitar
  `{ force: true }` no corpo (ou criar uma variante dedicada, o que for mais
  natural dado o código atual de `enrichEntity` em `api.ts`).
- `web/src/components/entity-manage-menu.tsx`: `DropdownMenuSeparator` +
  3 `DropdownMenuItem` novos, cada um com seu `useMutation`, ícone
  `lucide-react` (sugestão: `RefreshCw` para "Verificar Atualizações de
  Arquivos", `Download` ou `CloudDownload` para "Baixar Metadados",
  `Images`/`GalleryHorizontalEnd` para "Baixar Artes Faltantes" — ajustar
  conforme disponibilidade no pacote já instalado), label dinâmico durante
  `isPending`, mesma invalidação de `['entity', id]`/`['entities']`/
  `['artwork', id]` já usada pelos outros itens — acrescentar
  `['entity-files', id]` (ou o que o `getEntityFiles` usar como chave) já
  que "Verificar Atualizações de Arquivos" pode mudar a lista de arquivos.
  Sempre visíveis (sem condição de `catalogCompleteness`), diferente dos 2
  itens condicionais já existentes.
- Estender `web/src/components/entity-manage-menu.test.tsx` para cobrir os
  3 itens novos (render, clique dispara a chamada certa, feedback de
  sucesso/erro).

### Arquivos afetados

- `internal/db/migrations/0019_campos_locais_reproducao.sql` (novo).
- `internal/db/queries/entity.sql` (+ regenerar sqlcgen).
- `internal/scanner/*.go` (+ testes) — nova capacidade de recheck restrito.
- `internal/localmedia/nfo.go` — reaproveitado, sem mudança esperada (só
  consumido a partir de um novo caller).
- `internal/externaldetail/externaldetail.go` (+ teste) — `force`.
- `internal/enrich/enrich.go`, `internal/enrich/serie.go`,
  `internal/enrich/artes.go` (+ testes) — `force` propagado, wrapper de
  artes.
- `internal/server/server.go` (rotas novas), `internal/server/enrich.go`
  (body `force`), `internal/server/review.go`/`automatch.go`/
  `retroativo.go` (call sites de `Enriquecer` atualizados), novo(s)
  arquivo(s) para os 2 handlers novos.
- `openapi.yaml`.
- `web/src/lib/api.ts`, `web/src/components/entity-manage-menu.tsx` (+
  teste), tipos gerados (`gen:types`).

## 5. Critérios de aceite

- [x] `POST /entities/{id}/refresh-files` força re-hash/mediainfo dos
      arquivos da entity mesmo sem mudança de mtime/size (teste: arquivo
      idêntico no disco, `media_file` já gravado, forçar recheck e
      confirmar que a função de hash foi chamada de novo).
      (`TestRecheckEntityFilesForcaReHashMesmoSemMudanca`,
      `internal/scanner/recheck_test.go`;
      `TestRefreshEntityFilesImportaAssetLocalEExecutaRecheck`,
      `internal/server/refresh_files_test.go`; ponta-a-ponta via
      `TestJornadaAcoesForcadasGerenciar`, `internal/e2e/journeys_acoes_forcadas_test.go`.)
- [x] O mesmo endpoint importa/atualiza um asset `origin='local'` novo
      colocado na pasta de uma entity **já catalogada** (cenário hoje
      quebrado — confirmado na investigação que só funciona na 1ª
      varredura). (`TestRefreshLocalAssetDescobrePosterEmEntityJaCatalogada`,
      `internal/scanner/recheck_test.go`;
      `TestRefreshEntityFilesImportaAssetLocalEExecutaRecheck`,
      `internal/server/refresh_files_test.go`.)
- [x] Com um `.nfo` presente em `entity.nfo_path` trazendo `playcount`/
      `watched`/`userrating`/`resume`, e o banco ainda sem valor nesses
      campos, o mesmo endpoint preenche `entity.play_count`/`watched`/
      `user_rating`/`resume_position_seconds`/`resume_total_seconds`.
      (`TestRefreshEntityFilesPreencheCamposLocaisAusentes`,
      `internal/server/refresh_files_test.go`.)
- [x] Rodar o endpoint de novo com um valor JÁ presente no banco (diferente
      do que está no `.nfo`) NÃO sobrescreve — o campo já preenchido
      continua com o valor anterior (não-destrutivo).
      (`TestRefreshEntityFilesNaoSobrescreveCampoJaPreenchido`,
      `internal/server/refresh_files_test.go`.)
- [x] `POST /entities/{id}/enrich` com `{"force": true}` busca o detalhe do
      provider mesmo com um cache `external_detail` fresco (< 24h) para o
      mesmo `source`/`source_id` — e o corpo sem `force` (ou `force:
      false`) continua servindo do cache normalmente (sem regressão).
      (`TestForcePulaCacheFrescoEAtualiza`,
      `internal/externaldetail/externaldetail_test.go`; propagação
      ponta-a-ponta via `TestJornadaAcoesForcadasGerenciar`,
      `internal/e2e/journeys_acoes_forcadas_test.go`; corpo `{ force: true }`
      enviado pelo front coberto por
      `'envia { force: true } para POST /enrich e mostra "Baixando…"/sucesso'`,
      `web/src/components/entity-manage-menu.test.tsx`.)
- [x] `POST /entities/{id}/materialize-missing-media` atualiza o inventário
      `media_external(source='fanarttv')` da entity (nova consulta ao
      provider) e materializa na pasta real qualquer arte/​nfo que estivesse
      faltando, reaproveitando `MaterializarMidiasFaltantes` sem duplicar
      lógica. (`TestMaterializeMissingMediaAtualizaInventarioEMaterializa`,
      `TestMaterializeMissingMediaSemEnrichDegradaESoMaterializa`,
      `internal/server/materialize_missing_media_test.go`.)
- [x] Os 3 itens aparecem sempre no menu "Gerenciar" (sem depender de
      `catalog_completeness`), com feedback de carregando/sucesso/erro no
      mesmo padrão dos itens já existentes.
      (`'as 3 ações forçadas (0088) sempre aparecem, mesmo sem catalog_completeness'`,
      `'nenhum item condicional aparece quando catalog_completeness.complete é
      true, mas as 3 ações forçadas (0088) continuam aparecendo'`, e os
      `describe`s `"Verificar Atualizações de Arquivos"`/`"Baixar
      Metadados"`/`"Baixar Artes Faltantes"` (pendente/sucesso/erro cada),
      `web/src/components/entity-manage-menu.test.tsx`.)
- [x] `go build ./... && go vet ./... && go test ./... -count=1` verde.
- [x] `tsc -b` verde, `npm run lint` verde, `npm run test:coverage` verde.
- [x] `make gate-local` passa.

## 6. Notas de implementação / decisões

- **Por que campos locais de reprodução viram colunas diretas em `entity`,
  não passam pelo resolver/override?** Não são campo de catálogo
  (multi-idioma, multi-fonte) — são estado de reprodução local, sem
  equivalente em nenhum provider externo. Mesmo tratamento dado a
  `local_path`/`nfo_path` no briefing 0062: direto na tabela, fora do
  resolver de 3 camadas.
- **Por que só preencher quando ausente (`COALESCE`), nunca sobrescrever?**
  Coerente com os princípios 1 ("override do dono vence") e 5
  ("não-destrutivo") do `AGENTS.md`: se o banco já tem um valor, ELE é a
  fonte de verdade local — não existe "fonte externa" aqui pra ele perder
  disputa de precedência contra. O `.nfo` só serve pra resgatar dado que
  nunca chegou ao banco (ex.: pasta migrada de outro gerenciador que já
  tinha histórico de reprodução gravado no `.nfo`).
- **Por que não expor edição desses campos pela UI agora?** Fora do pedido
  original (que era sobre os 3 botões de "Gerenciar"); UI de watch-status é
  feature própria, com decisões de UX que não foram discutidas aqui
  (marcar como assistido de onde? por episódio? em lote?).
- **Por que "Baixar Artes Faltantes" não cascateia pra filhos de série?**
  Mesmo padrão de todo o resto do menu Gerenciar hoje — cada ação vale pra
  entity aberta. Cascata automática misturaria o escopo "ação pontual do
  dono" com "reprocessamento em lote", que já é o papel do job periódico.
- **Por que `force` em `Enriquecer` em vez de um método novo
  `EnriquecerForcado`?** Menos superfície de API — um parâmetro booleano
  adicional é mais simples de manter que duplicar o método inteiro, e todo
  call site já precisa passar pela assinatura de qualquer forma.

## 7. Fora deste briefing / follow-ups

- UI para visualizar/editar os campos locais de reprodução no detalhe da
  entity.
- Cascata opcional ("aplicar em todos os episódios desta temporada/série")
  para as 3 ações — só se o dono sentir falta depois de usar a versão
  pontual.
- Fallback do "Verificar Atualizações de Arquivos" para pastas de série
  (hoje o scan de série não descobre poster local nenhum, nem na criação —
  gap pré-existente, fora do escopo deste fix pontual; o `refresh-files`
  deste briefing busca direto na pasta do arquivo sem depender do scanner
  de série, mas não corrige o scanner de série em si).
