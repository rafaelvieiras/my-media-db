# 0029 — MD6: elenco, equipe e produtoras (person / credit / company)

> Fecha a última lacuna do épico "tela rica": o **elenco e a equipe** (cast/crew) e
> as **produtoras/estúdios** do item — modelados como entidades próprias
> (`person`/`company`) ligadas à obra por **créditos** (`credit`), com override de
> créditos **não-destrutivo** e paridade de exibição entre o detalhe do externo e o da
> entity. É **GRANDE** (schema novo + provider + resolver + API + UI); por isso nasce
> **faseado**.

| | |
|---|---|
| **ID** | 0029 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override (extensão pós-revisão) |
| **Docs relacionadas** | [04-modelo-de-dados](../docs/04-modelo-de-dados.md), [07-enriquecimento-providers](../docs/07-enriquecimento-providers.md), [05-api](../docs/05-api.md), [ADR-0012](../docs/adr/0012-camadas-externo-override.md) |
| **Status** | in-progress (A+B+D done; C edição de créditos pendente) |
| **Depende de** | [0012](0012-schema-fase3-camadas-externo-override.md), [0013](0013-resolver-tres-camadas.md), [0022](0022-detalhe-externo-rico-persistente.md), [0023](0023-tela-detalhe-externo-ui.md), [0024](0024-paridade-detalhe-entity-rico.md) |

---

## 1. Objetivo

Hoje a tela rica (externo e entity) mostra o item por inteiro **menos** quem o fez: o
**elenco** (atores + personagens) e a **equipe** (diretor, roteiro, fotografia…), além
das **produtoras**. Foi o item **explicitamente adiado** no [0012](0012-schema-fase3-camadas-externo-override.md)
(§7) por exigir um **submodelo novo** — não cabe num campo simples nem numa lista de
strings como as tags.

O elenco/equipe tem estrutura própria: uma **pessoa** aparece em **muitas** obras, uma
**obra** tem **muitas** pessoas, e a ligação carrega **atributos** (papel, personagem,
departamento, ordem de bilhagem). É uma relação **N:N com dados na aresta** — o
**crédito**. Modelar assim (em vez de enfiar num `extension_data` opaco) permite, no
futuro, **navegar por pessoa** ("outros filmes do mesmo diretor"), deduplicar por id de
fonte e manter a mesma garantia **não-destrutiva** do resolver de 3 camadas (ADR-0012):
o dono pode corrigir/rearranjar créditos sem que o re-enriquecimento os sobrescreva.

A dor concreta do usuário: abrir o detalhe (de um item externo ou já catalogado) e ver
"Elenco" e "Equipe" como no TMDB, com pôsteres/fotos, papéis e navegação — e poder
**ajustar** quando a fonte erra (ordem, um crédito faltando, um nome trocado).

## 2. Jornada do usuário

1. O dono abre o detalhe de um item (externo **ou** entity). Abaixo dos blocos atuais
   (sinopse, gêneros, vídeos, coleção, similares) surgem duas seções novas: **Elenco**
   (atores com foto, nome e personagem, na ordem de bilhagem) e **Equipe** (agrupada por
   departamento: Direção, Roteiro, Fotografia…). Uma faixa **Produtoras** lista os
   estúdios (logo/nome).
2. O elenco/equipe respeita o **idioma ativo**: o *personagem* e o *departamento/cargo*
   podem ser traduzíveis; o **nome da pessoa** é neutro (não se traduz um nome próprio).
3. Clicar num **ator/membro** (follow-up) abre a **página da pessoa** com a filmografia
   dela — nesta versão o card é, no máximo, um link para a fonte (TMDB) e/ou um destino
   interno quando a pessoa já existe no catálogo (interno-first, [0026](0026-centralizacao-interno-first.md)).
4. No detalhe da **entity** (catálogo), em modo **Editar**, o dono ajusta os créditos:
   **reordenar** o elenco, **ocultar/remover** um crédito da fonte, **adicionar** um
   crédito manual, **corrigir** o personagem/cargo. Salvar grava um **override de
   créditos**; **Limpar** volta ao que a fonte diz (não-destrutivo).
5. **Re-enriquecer** puxa o elenco/equipe atualizado da fonte **sem** desfazer os ajustes
   do dono (mesma garantia do título/tags — ADR-0012).
6. Estados: item sem créditos (fonte não trouxe) → a seção some; fonte indisponível →
   as demais seções seguem servíveis (o elenco degrada, não derruba a leitura).

## 2.5. Rodada atual — Fases A+B (decisões travadas 2026-07-12)

Esta rodada entrega **A (schema+persistência)** e **B (provider+exibição read-only)**;
**C (edição de créditos)** e **D (interno-first/polimento)** ficam para depois.

- **Formas reais do TMDB** (confirmadas ao vivo): filme `/movie/{id}?append_to_response=credits`
  → `credits.cast[]{name,character,order,profile_path}` + `credits.crew[]{name,department,job,profile_path}`
  + `production_companies[]{name,logo_path}`. Série `/tv/{id}?append_to_response=aggregate_credits`
  → `aggregate_credits.cast[]{name,roles:[{character,episode_count}],total_episode_count,order}`
  + `crew[]{name,jobs:[{job,episode_count}],department}`.
- **Série agrega por episódio**: normalizar `roles`→personagem (unir múltiplos papéis do mesmo
  ator: "Peter / Brian / Stewie (voz)") + `total_episode_count`; `jobs`→job + episode_count.
  O canônico do crédito ganha um `episode_count` opcional (só série).
- **Teto**: listas enormes (filme 76, série 781). **Persistir top ~50** do elenco por
  `order` (bilhagem); a UI exibe **top ~24 + "ver todos"**. Equipe agrupada por
  departamento. Companies: todas.
- **Fotos/logos**: servidas pelo **proxy de imagem** (image.tmdb.org já na allowlist),
  como pôster/arte — não a URL crua.
- **Read-only nesta rodada**: sem editor de créditos (C) nem `internal_id` de pessoa (D).

## 3. Escopo

**Dentro:**
- **Schema**: tabelas `person`, `company` e `credit` (a aresta obra↔pessoa/empresa), com
  ligação `credit.entity_id → entity.id` e chaves de fonte para dedup.
- **Provider (TMDB)**: acrescentar `credits`/`aggregate_credits` e `images` de pessoa ao
  `append_to_response` e mapear para um **canônico de créditos** (nomes dos NOSSOS
  campos), como o [0022](0022-detalhe-externo-rico-persistente.md) fez para vídeos/coleção/similares.
- **Resolver**: compor os créditos pelas 3 camadas (arquivo⟵externo⟵override) como uma
  **lista estruturada** com merge por operações (reordenar/remover/adicionar/editar),
  espelhando a filosofia do `comporLista`/`comporExtensao` (R3/R5), guiada pelo registro
  declarativo (sem `switch` por nome — R1/ADR-0001).
- **API**: expor `cast`/`crew`/`companies` no `ResolvedEntity` e no `ExternalDetail`
  (mesmo shape, via um pacote compartilhado como o `internal/richfields`); endpoints de
  override de créditos.
- **UI**: seções **Elenco/Equipe/Produtoras** na `RichDetailView` compartilhada (paridade
  externo × entity) + editor de créditos no modo Editar da entity.

**Fora (explicitamente):**
- **Página da pessoa** (filmografia navegável, `GET /person/...`) — follow-up; aqui o
  card de pessoa é no máximo link externo + `internal_id` quando já catalogada.
- **Importar uma pessoa como entity** própria (catalogar pessoas) — não nesta fase.
- **Companies como entity** navegável (produtora com catálogo) — só exibição aqui.
- **Busca por pessoa/produtora** e filtros do catálogo por elenco — follow-up.
- **Fotos/artes de pessoa cacheadas localmente** (proxy/cache como o pôster do
  [0015](0015-enriquecimento-import-proxy-imagem.md)) — nesta versão usa a URL direta da
  fonte; o cache é follow-up.

## 4. Contrato técnico

> Preencher na implementação de cada fase; abaixo, o desenho-alvo.

### Dados / modelo

Três tabelas novas (migração **goose em pt-BR**; comentários em pt-BR):

- **`person`** — a pessoa (ator/membro de equipe), **desacoplada** da obra:
  `id` (TEXT, nosso), `source`, `source_id` (dedup por fonte, `UNIQUE(source, source_id)`),
  `name` (neutro), `profile_path`/`profile_url` (foto), `payload_json` (cru re-componível,
  ADR-0011). Uma pessoa é **compartilhada** por vários créditos.
- **`company`** — produtora/estúdio: `id`, `source`, `source_id`
  (`UNIQUE(source, source_id)`), `name`, `logo_url`, `payload_json`.
- **`credit`** — a **aresta** obra↔pessoa/empresa, onde moram os atributos do vínculo:
  `id`, `entity_id → entity.id ON DELETE CASCADE`, `person_id → person.id` (nulo para
  crédito de company), `company_id → company.id` (nulo para crédito de pessoa),
  `kind` (`cast` | `crew` | `company`), `department` (crew: Direção/Roteiro/…),
  `job`/`role` (cargo), `character` (elenco), `order` (bilhagem/ordem estável),
  `source`, `source_id`. Índice por `entity_id` (lookup do detalhe) e por `person_id`
  (filmografia futura).

Decisões de schema a travar (espelham o [0012](0012-schema-fase3-camadas-externo-override.md)):
- **Cache re-componível**: `credit`/`person`/`company` derivam do `payload_json` do
  `media_external`/`external_detail` — podem ser **regenerados** sem perda de dado do
  dono (o dado do dono vive no **override**, não nestas tabelas).
- **Invariante de identidade** (análogo ao MD4): `credit.source_id` corresponde ao id do
  crédito na fonte; a app mantém, o schema não o expressa como constraint portável.
- **FKs `ON DELETE CASCADE`** só na purga da entity; pessoas/empresas **não** somem quando
  um crédito é removido (são compartilhadas).

### Override de créditos (o coração não-destrutivo)

O override entra como **mais uma chave** no `media_override.override_json` (blob esparso,
schemaless — **sem migração** no lado do override, igual à **nota local** recém-adicionada,
que também é só uma chave do blob resolvida por um kind próprio). Proposta de forma
canônica (a validar na Fase C):

```jsonc
"credits": {
  "reorder": ["credit_src_1", "credit_src_5", ...],   // nova ordem de bilhagem
  "remove":  ["credit_src_9"],                          // ocultar um crédito da fonte
  "add":     [ { "kind":"cast", "name":"…", "character":"…", "order": 3 } ],
  "edit":    { "credit_src_2": { "character":"…", "job":"…" } }  // correções pontuais
}
```

- Merge por **operações** sobre a base externa (R3/ADR-0003), como as tags — a base da
  fonte segue **viva por baixo**; o override só descreve o **delta**.
- Campos traduzíveis do crédito (`character`, `department`/`job`) compõem **por idioma**
  quando houver contexto de idioma (ADR-0018/0019), como title/tags; o **nome da pessoa**
  é neutro.
- Novo **kind de composição** no registro do resolver (ex.: `kindCreditos`) — o motor
  despacha por kind, não por nome de campo (R1). `CamposEditaveis()` passa a incluir
  `credits` para o editor validar a forma do patch (como já faz com `local_rating`).

### API

- **Leitura** — acrescentar ao `ResolvedEntity` e ao `ExternalDetail` (mesmo shape, via um
  módulo compartilhado tipo `internal/richfields` — provavelmente **estendê-lo** ou criar
  um `internal/credits`):
  - `cast`: `[{ person_id?, internal_id?, source_id, name, character, order, profile_url }]`
  - `crew`: `[{ person_id?, internal_id?, source_id, name, department, job, profile_url }]`
  - `companies`: `[{ source_id, internal_id?, name, logo_url }]`
- **Override** — reusar a superfície do editor ([0016](0016-editor-override-ui.md)):
  `PATCH /entities/{id}/override` com a chave `credits` (o handler já é genérico — só
  precisa a nova forma valer em `formaValida`); `DELETE /entities/{id}/override/credits`
  para limpar. **Sem** endpoint novo dedicado se o blob genérico bastar.
- Erros: 422 para forma de `credits` inválida; a fonte indisponível degrada o bloco (não
  502 a leitura inteira).

### Backend (Go)

- `internal/db/migrations/00NN_elenco_equipe.sql` (pt-BR) + queries sqlc em **ASCII**
  (regra do projeto — sqlc corrompe acento em `.sql` de query); `sqlc generate` após.
- `internal/provider/tmdb`: estender `append_to_response` (hoje
  `translations,videos,keywords,release_dates,content_ratings,external_ids,similar`) com
  `credits` (filme) / `aggregate_credits` (série) e mapear em `tmdb_rico.go` para o
  canônico de créditos (traduzíveis como `map[idioma]valor`; neutros escalares).
- `internal/richfields` (ou novo `internal/credits`): tipos `Cast`/`Crew`/`Company` +
  extratores **puros** sobre o canônico, compartilhados pelo handler do externo e pelo
  resolver da entity (fonte única — evita as duas telas divergirem).
- `internal/resolver`: novo kind + compositor de créditos; entrada no `registroOrigem`.
- `internal/server`: persistência das tabelas no import/enrich; `anotarInternos`
  preenche `internal_id` de pessoa/company já catalogada (interno-first, [0026](0026-centralizacao-interno-first.md)).

### Frontend (Vite/React)

- `openapi.yaml` + `npm run gen:types` (saída **formatada**; o hook do prettier roda da
  raiz): novos schemas `Cast`/`Crew`/`Company` e os campos no `ResolvedEntity`/`ExternalDetail`.
- `web/src/lib/rich-detail.ts`: estender `RichDetail` com `cast`/`crew`/`companies` e os
  dois adapters (`deExternalDetail`/`deResolvedEntity`).
- `web/src/components/rich-detail-view.tsx`: seções **Elenco** (grid de fotos), **Equipe**
  (agrupada por departamento) e **Produtoras**; card de pessoa reusável (foto+nome+papel),
  interno-first no destino do link.
- `web/src/components/override-editor.tsx`: sub-editor de créditos (reordenar via
  drag/handles ou setas, remover, adicionar, corrigir) → monta o patch `credits`;
  botão **Limpar** (DELETE do campo).

### Arquivos afetados (previsão)

- Novos: `internal/db/migrations/00NN_elenco_equipe.sql`,
  `internal/db/queries/credit.sql` (+ person/company), possivelmente `internal/credits/*`.
- Alterados: `internal/provider/tmdb/tmdb.go` + `tmdb_rico.go`, `internal/resolver/resolver.go`,
  `internal/server/{entities,enrich,external_detail,override}.go`, `openapi.yaml`,
  `web/src/lib/{api-types,rich-detail}.ts`, `web/src/components/{rich-detail-view,override-editor}.tsx`.

## 5. Critérios de aceite

- [ ] Importar/enriquecer um filme popular popula `person`/`credit`/`company` e o
      `ResolvedEntity` traz `cast`/`crew`/`companies` não vazios, na ordem de bilhagem.
- [ ] O **mesmo shape** de créditos sai no `ExternalDetail` (antes de importar) e no
      `ResolvedEntity` (catálogo) — paridade garantida por módulo compartilhado.
- [ ] Uma pessoa que aparece em duas obras tem **uma** linha em `person` (dedup por
      `source_id`), com dois créditos.
- [ ] Override de créditos: reordenar/remover/adicionar/corrigir grava o delta; a base da
      fonte segue viva por baixo; **re-enriquecer não desfaz** o ajuste (jornada E2E
      Camada 1, como a do enrich não-destrutivo).
- [ ] Limpar (`DELETE …/override/credits`) volta ao elenco/equipe da fonte
      (não-destrutivo).
- [ ] `character`/`department` respeitam o idioma ativo; o **nome** da pessoa é neutro.
- [ ] Forma de `credits` inválida → 422; fonte indisponível **degrada** o bloco sem
      derrubar a leitura.
- [ ] `internal_id` preenchido para pessoa/company já catalogada (interno-first).
- [ ] UI: seções Elenco/Equipe/Produtoras aparecem nas duas telas; editor de créditos no
      modo Editar; estados vazio/carregando/erro.
- [ ] Catraca (`quality-gate.mjs --local`) PASSA; cobertura mantida/melhorada
      (`make baseline` se melhorou).

## 6. Notas de implementação / decisões

- **Por que `credit` como aresta com atributos** (e não `extension_data`): a relação é
  N:N **com dados na ligação** e precisa de **dedup de pessoa** e **navegação futura por
  pessoa** — um blob opaco no `extension_data` não deduplicaria nem indexaria. Modelar a
  aresta é o que abre a página da pessoa e a garantia não-destrutiva sem gambiarra.
- **Override no blob, tabelas re-componíveis**: o dado do dono NUNCA vive em
  `person`/`credit`/`company` (que são cache da fonte, regeneráveis); vive no
  `media_override.override_json`. Assim o re-enriquecimento pode **apagar e repopular** as
  tabelas de cache sem tocar o override — é o mesmo princípio do ADR-0012 que a nota local
  já exercita, agora numa lista estruturada.
- **Faseamento sugerido** (cada fase entrega valor e testa isolada):
  - **Fase A — schema + acesso a dados**: migração + queries sqlc + persistência no
    import/enrich, **sem** resolver/UI (fundação isolada e testável, como o [0012](0012-schema-fase3-camadas-externo-override.md)).
  - **Fase B — provider + canônico + leitura**: mapear créditos no TMDB e **expor**
    `cast`/`crew`/`companies` (read-only) no `ExternalDetail` e no `ResolvedEntity` via
    módulo compartilhado. UI de **exibição** já entra (paridade), **sem** edição.
  - **Fase C — override de créditos**: novo kind no resolver + forma canônica do patch +
    validação no server + editor na UI. É a fase de maior risco de design (a forma do
    merge) — travar com testes de resolver e uma jornada E2E não-destrutiva.
  - **Fase D — interno-first + polimento**: `internal_id` de pessoa/company, agrupamento
    de equipe por departamento, estados vazios, acessibilidade.
- **Traduzível × neutro**: seguir ADR-0018/0019 — `character` e `job/department` compõem
  por idioma; `name` é neutro. O TMDB localiza personagem via `credits` no idioma pedido;
  a equipe/`department` é semi-fixa (mapear rótulos no front quando a fonte não traduz).
- **Reuso do editor de override existente**: o handler `PATCH/DELETE /override` já é
  genérico (valida contra `resolver.CamposEditaveis()`); acrescentar `credits` é
  principalmente **forma + composição**, não um endpoint novo — preferir isso a inventar
  superfície.

## 7. Fora deste briefing / follow-ups

- **Página da pessoa** (`GET /person/{source}/{id}` + rota) com filmografia navegável.
- **Catalogar pessoa/produtora** como entity própria (interno-first pleno, além do link).
- **Cache local de fotos/logos** (proxy como o do pôster, [0015](0015-enriquecimento-import-proxy-imagem.md)).
- **Busca e filtros por elenco/equipe/produtora** no catálogo.
- **Jellyfin/consumo externo** dos créditos (se algum consumidor os exigir).
