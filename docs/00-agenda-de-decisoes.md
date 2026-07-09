# 00 — Agenda de decisões (documento vivo)

> **O que é:** o backlog das decisões de **fundação** que ainda precisam ser
> tomadas ou documentadas. Não é especificação — é a lista de trabalho. Cada item
> aqui, quando decidido, vira texto nas docs de spec (`04`, `03`, …) ou num ADR.
>
> **Como trabalhamos:** um item por vez. Para cada item, o fluxo é:
> 1. Contexto do problema + resumo para leigos.
> 2. Perguntas objetivas (com opções sugeridas) até a decisão fechar.
> 3. A decisão é registrada na coluna **Decisão** abaixo e promovida ao documento
>    de destino.
>
> **Status possíveis:** `aberto` · `em discussão` · `decidido` · `documentado`.

---

## Foco atual: especificação do resolver

O resolver compõe o valor final de cada entidade a partir de três camadas
(`override ⟵ externo ⟵ arquivo`). É o coração do sistema e hoje está subespecificado.
Estas são as decisões que fecham a spec dele.

| ID | Questão | Status | Decisão | Destino |
|---|---|---|---|---|
| **R1** | As camadas competem pelo mesmo campo ou o espaço de campos é particionado por origem? | **documentado** | Particionar por origem. A origem é **metadado do campo**, não hard-code: núcleo declara no schema do núcleo; extensões/plugins declaram no JSON Schema do próprio namespace. Resolver é motor **genérico** (não conhece campo por nome). A anotação declara só as **fontes-base** (`arquivo`/`externo`/`nenhuma`); **override é universal** (nunca declarado). Campos de sistema (`id`, `created_at`, `content_hash`) ficam fora do resolver. | `04` (+ nova seção resolver) |
| **R2** | Com duas fontes externas (TMDB+TVDB) no mesmo campo, quem vence? | **documentado** | **Lista de prioridade global configurável** de fontes (ex.: `[tmdb, tvdb, …]`); por campo, vence a primeira fonte da lista que tiver valor — antes do override. Default MVP: `[tmdb]`. Alternativas (prioridade por campo, fonte primária por entidade) registradas como evolução pós-MVP. | `04` / ADR |
| **R3** | Override em campos-lista (tags, gêneros, elenco): substitui a lista inteira ou faz merge? | **documentado** | **Merge por operações** (não substituição). O override guarda um conjunto de **adições** e um de **remoções por valor**; a base externa segue viva por baixo (re-scrape evolui o resto). Ordem = externa, com remoções aplicadas e adições ao final; dedup ao compor. Vale **só para listas de valores simples** (`tags`, `genres`). Elenco/equipe (relações) → ver MD6. | `04` |
| **R4** | Armazenamento do override: EAV `(entity_id, field, value)` vs blob JSON esparso. **(mexe na tabela da doc 04)** | **documentado** | **Blob JSON esparso**: `media_override(entity_id, override_json, updated_at)` — uma linha por entidade, JSON espelhando o formato da entity, só com campos mudados. Validável contra o mesmo schema (núcleo+plugins, R1); comporta o `{add,remove}` do R3 e override dentro de `extension_data` (R5). Resolver = base composta + aplica patch. **Substitui o esboço EAV na doc 04.** | `04` / ADR |
| **R5** | `extension_data` também passa pelo resolver (composição uniforme)? | **documentado** | **Sim, composição uniforme.** Núcleo e extensão passam pelo mesmo resolver; `override ⟵ externo ⟵ arquivo` vale campo a campo, inclusive dentro dos namespaces de `extension_data`. Coerente com R1 (motor genérico) e R4 (blob espelha a entity inteira). | `04` |
| **R6** | Como o dono "apaga" um valor — distinguir "não fiz override" de "override para vazio"? | **documentado** | **Presença da chave = override ativo**, mesmo com `null`/vazio (apaga o valor externo). Chave ausente = herda da base. A UI precisa separar "limpar override" (remove a chave) de "definir vazio" (chave com `null`). **Proveniência por campo não é exposta na API por ora** — a UI de edição consulta o override separadamente; expor proveniência fica como possível evolução (Fase 4). | `04` |
| **R7** | Fatos técnicos (mediainfo) são overridáveis pelo dono? | **documentado** | **Sim**, pelo mesmo mecanismo universal (R1) — sem regra especial. O dado técnico detectado permanece em `media_file` (não-destrutivo); o override entra por cima na composição. Uso raro (corrigir detecção errada do ffprobe), mas permitido. | `04` |

---

## Modelo de dados (fora do resolver, mas de fundação)

| ID | Questão | Status | Decisão | Destino |
|---|---|---|---|---|
| **MD1** | Identidade e matching: como um arquivo vira/associa a uma `Entity` e a um id externo? Um filme com múltiplos arquivos (edições/qualidades) — relação 1:N normativa. | **documentado** | Adota a **convenção Kodi/Jellyfin** (padrão de-facto): **1:N plano** (1 obra = 1 `Entity`; qualidades/edições/partes = `media_file`), **edição = rótulo no arquivo**, **id externo embutido no nome = chave determinística**, *stacking* de partes. Match carrega **score de confiança**: alta casa sozinho, baixa **casa mesmo assim + alerta de revisão** (*"mapeado com 60% de certeza. Remover?"*), zero resultados vira `draft` não-identificado; correção do dono é autoritativa (override). Verticais fora do padrão (videoclipe→IMVDB, quadrinhos→ComicInfo) trazem identidade própria via `extension_data`, sem tocar no núcleo. | `06` (+ `04`) |
| **MD2** | `status` (intenção do dono) vs posse (existe `media_file`?) estão conflados na doc 04. Separar em eixos ortogonais. | **documentado** | **Dois eixos ortogonais.** **Posse = fato derivado** (existe `media_file`?), não editável, não mente. **`status` = intenção do dono**, um estado por vez: `cataloged` (default), `wanted`, `unreleased`, `draft`. Removido o `owned` (era posse disfarçada). Combinam livre (ex.: `wanted`+posse=sim = quero upgrade tendo cópia). Transições entre estados → MD5. | `04` |
| **MD3** | Concorrência no SQLite: API sempre no ar enquanto o scan escreve pesado (single-writer). Estratégia (WAL + escritor único). | **documentado** | Receita em 3 camadas. **(1) WAL + pragmas**: `journal_mode=WAL`, `busy_timeout=5000`, `synchronous=NORMAL`, `foreign_keys=ON` — vários leitores + 1 escritor sem bloqueio mútuo. **(2) Dois pools**: pool de N leitores + **1 conexão de escrita** (escritor único). **(3) Escritas curtas, dono na frente**: scan comita em lotes curtos; escrita interativa (override/import) tem prioridade sobre o scan. Evolução: fila/goroutine escritora se precisar priorização explícita. | `03` |
| **MD4** | Identidade externa duplicada em `entity.external_ids` e `media_external.(source, source_id)`. Qual é a autoritativa? | **documentado** | **`entity.external_ids` é o mapa de identidade autoritativo** (todos os ids conhecidos, buscados ou não; é onde o match do MD1 grava e o lookup lê). **`media_external` é cache de payload**, `source_id` subordinado (invariante: bate com `external_ids[source]`). **Conflito → identidade vence**: linha divergente é cache obsoleto → re-busca/descarta; o dono corrige a identidade, nunca edita `media_external` à mão. Identidade ≠ valor editorial (media_external segue base do resolver). Mesma lógica p/ `person`/`company`. | `04` |
| **MD5** | Ciclo de vida / deleção: arquivo some do disco — a entity vira `wanted`? órfã? Comportamento não-destrutivo definido. | **documentado** | **Evento de arquivo mexe só na posse; nunca no `status` nem no trabalho do dono.** Arquivo some (datasource acessível) → `media_file` vira **`missing`** (guarda `last_seen`), entity preservada, posse→não, status **intacto**. **Datasource inteiro offline** (HD desmontado) → nada vira `missing`, datasource sinalizado offline. Arquivo aparece p/ `wanted` → posse=sim, segue `wanted`. **Órfã** = entity auto-criada, arquivo sumiu, **zero investimento do dono** → fila de limpeza, **nunca apagada sozinha**. **Purga** é a única operação destrutiva, sempre explícita. | `04` (+ `06`) |
| **MD6** | Override de **relações** (elenco/equipe): merge por `person_id`, edição de papel/ordem. Separado do R3 por ser bem mais complexo (tabelas `person`/`credit`). | **documentado** | **R3 generalizado para créditos estruturados.** Override guarda **operações** (add/remove/edit de papel/`ord`); base externa (`credit` do payload) segue viva e evolui em re-scrape. **Chave = `person_id` + papel/departamento** (distingue a mesma pessoa em 2 papéis), com `credit_id` do provider como desempate. Pessoa adicionada pelo dono vira `person` **local** (`external_ids` vazio, ok por MD4). Resolver compõe crédito a crédito (R5); ops vivem no `media_override` (R4). | `04` |

---

## Idiomas (multi-idioma) — fundacional, levantado na Fase 3

O acervo é multi-idioma: o gestor define principal + fallback + permitidos, todo
valor carrega a flag do idioma, e o dono acrescenta traduções. Reformula o modelo
das três camadas antes de o override (0016) aterrissar.

| ID | Questão | Status | Decisão | Destino |
|---|---|---|---|---|
| **L1** | Como o idioma entra no modelo das três camadas? Onde vive a config (principal/fallback/permitidos)? | **documentado** | **Dimensão de idioma = CHAVE dentro de cada camada** (externo tagueia por idioma no payload `/translations`; override fica `campo→idioma→valor`; núcleo é o fallback neutro), sem tabela nova. Campos traduzíveis (`title`/`description`/`tags`) vs neutros (`original_title`/`original_language`/`release_date`/`external_ids`/técnicos). Nova coluna `entity.original_language`. **Config em settings no banco** (`app_settings`, chave `languages`) com UI de admin — não em env. | [ADR-0018](adr/0018-modelo-multi-idioma-dimensao-nas-camadas.md) |
| **L2** | Qual idioma a API serve e o que expõe sobre isso? | **documentado** | **Resolve para UM idioma** por requisição (`?lang=`, default = principal), shape **plano** preservado; **cadeia de fallback** `pedido → fallback → original → arquivo`; **autoridade do dono por idioma** (R6 por idioma; `null` no pedido interrompe o fallback); **proveniência de idioma** exposta (`resolved_languages`). | [ADR-0019](adr/0019-resolucao-por-idioma-fallback-proveniencia.md) |
| **L3** | Como traduzir tags (o TMDB não traduz keywords)? | **documentado** | **Tags viram lista por idioma**, merge de operações (R3) **dentro de cada idioma**; **gêneros** pela lista localizada do TMDB por idioma (cacheável); **keywords** guardadas no idioma obtido, tradução **manual** pelo dono (sem tradução automática). | [ADR-0020](adr/0020-traducao-de-tags-genres-keywords.md) |

---

## Melhorias de base (estrutura da documentação)

| ID | Item | Status | Decisão | Destino |
|---|---|---|---|---|
| **ME1** | Adotar ADRs (um arquivo por decisão) em vez da lista achatada da doc 11. | **documentado** | **Sim, formato leve.** Criado `docs/adr/` com README (índice + prática), `template.md` (Contexto·Decisão·Consequências) e **13 ADRs** migrados das decisões fechadas (R1–R7 → 0001–0007; MD1–MD6 → 0008–0013). ADR é **imutável**: nova decisão que supera outra cria novo ADR marcando `Substituída por`. | `docs/adr/` |
| **ME2** | Diagrama ER (mermaid) do modelo de dados. | **documentado** | Diagrama `erDiagram` (mermaid) na doc 04, após o esboço de tabelas: entity + as 3 camadas do resolver (`media_file`/`media_external`/`media_override`) + `credit`/`person`/`asset`/`entity_relation`/`library_source`. `company` sem aresta rígida (join deixado ao briefing de schema). | `04` |
| **ME3** | Criar `LICENSE` e `CONTRIBUTING.md` (inclinação MIT/Apache-2.0). | **documentado** | **MIT.** `LICENSE` (MIT, © 2026 Rafael Vieiras e contribuidores) + `CONTRIBUTING.md` na raiz (princípios, fluxo decisão→ADR→spec, commits em pt-BR, segredos nunca no código). | raiz |
| **ME4** | Glossário: termos de precedência do resolver e "posse" vs "status". | **documentado** | Doc 02 atualizada: `status` corrigido (intenção, sem `owned`), **Posse** adicionada (fato derivado), + termos Precedência do resolver, Override universal, Merge por operações, Proveniência, `missing`, Órfã, Purga, `external_ids`/`media_external`. | `02` |

---

## Histórico de decisões fechadas

<!-- Conforme cada item fecha, mover o resumo da decisão para cá com data. -->

- **L1–L3 — Fundação multi-idioma** (2026-07-09). Requisito fundacional levantado na
  Fase 3 e ausente do design inicial: o acervo é multi-idioma. **L1 (ADR-0018):** a
  dimensão de idioma é a **chave dentro de cada camada** (payload externo tagueado
  por idioma via TMDB `/translations`; override `campo→idioma→valor`; núcleo como
  fallback neutro), sem tabela nova; campos traduzíveis (`title`/`description`/`tags`)
  vs neutros; nova coluna `entity.original_language`; **config em settings no banco**
  (`app_settings`) com UI de admin. **L2 (ADR-0019):** a leitura **resolve para um
  idioma** (`?lang=`, default principal) mantendo o shape plano, com **cadeia de
  fallback** (`pedido → fallback → original → arquivo`), autoridade do dono **por
  idioma** (R6 por idioma; `null` no pedido não cai para o fallback) e
  **proveniência de idioma** exposta. **L3 (ADR-0020):** **tags por idioma** com
  merge de operações dentro de cada idioma — gêneros pela lista localizada do TMDB,
  keywords no idioma obtido com tradução manual. Implementação em briefings
  0019–0021 (fundação → provider → UI), reformando 0012/0013/0015 e antecedendo o
  0016 (override por idioma).
- **Doc 11 — Stack de implementação e passthrough do `/api/tmdb`** (2026-07-08).
  Fecha as decisões maduras da doc 11. **Stack (ADR-0014):** ratificadas as
  inclinações — router **chi**, driver **`modernc.org/sqlite`** (CGO-free), dados
  via **sqlc**, mediainfo via **ffprobe**, contrato de tipos via **OpenAPI gerado**;
  tudo coerente com "binário único, CGO-free, cross-compile trivial". **`/api/tmdb`
  (ADR-0015):** o dono refinou o default — **passthrough + cache, nunca 404 por
  indisponibilidade**; um *miss* de busca só cacheia, um *miss* de detalhe pode
  virar `draft` em fila de revisão (`promote_draft`) ou ficar em cache oculto com
  TTL (`hidden_cache`), à escolha; falha da fonte serve cache stale. Promovido à doc
  05. **Licença MIT** confirmada (já em ME3). Os demais itens da doc 11 (plugins,
  verticais, auth, layout de assets, scan incremental) foram reclassificados de
  "abertos" para **adiados por fase** — não são indecisões de fundação.
- **ME1–ME4 — Arrumação de base da documentação** (2026-07-08). Fecha o backlog de
  fundação. **ME1:** adotados **ADRs em formato leve** — `docs/adr/` com README
  (índice + prática), `template.md` e **13 ADRs** migrados das decisões já fechadas
  (R1–R7 = ADR-0001…0007; MD1–MD6 = ADR-0008…0013); ADR é imutável e uma decisão que
  supere outra vira novo ADR. **ME2:** **diagrama ER** (mermaid) na doc 04, com as
  três camadas do resolver penduradas da `entity`. **ME3:** licença **MIT** —
  `LICENSE` + `CONTRIBUTING.md` na raiz (fluxo decisão→ADR→spec, pt-BR, segredos via
  env). **ME4:** **glossário** (doc 02) atualizado — `status` corrigido (intenção,
  sem `owned`), **Posse** como fato derivado e os termos de precedência do resolver.
  Com isto, toda a fundação (R1–R7, MD1–MD6, ME1–ME4) está fechada e documentada.
- **MD3 — Concorrência SQLite: WAL + escritor único + escritas curtas** (2026-07-08).
  Resolve "API sempre no ar × scan escrevendo pesado" contra o limite de um escritor
  do SQLite. Receita em três camadas: **(1)** abrir em **WAL** com `busy_timeout=5000`,
  `synchronous=NORMAL` e `foreign_keys=ON` (vários leitores + um escritor sem bloqueio
  mútuo, sem `database is locked`); **(2)** **dois pools** — leitura com N conexões e
  **uma** conexão de escrita (escritor único); **(3)** scan em **lotes curtos** com
  escrita **interativa priorizada** sobre a do scan. Evolui para uma fila/goroutine
  escritora se a priorização precisar ser explícita. Promovido à doc 03 (Concorrência
  e persistência).
- **MD6 — Override de relações (elenco/equipe) por merge de operações** (2026-07-08).
  Fecha o que o R3 adiou: elenco/equipe seguem a mesma filosofia (merge de operações,
  base externa viva por baixo), mas sobre **créditos estruturados**. O override guarda
  **add/remove/edit** (papel, personagem, `ord` de billing); a **chave do crédito** é
  `person_id` + papel/departamento (a mesma pessoa em dois papéis = dois créditos),
  com o `credit_id` do provider como desempate. Pessoa adicionada pelo dono vira um
  `person` **local** sem id externo (coerente com MD4). O resolver compõe crédito a
  crédito (R5) e as operações vivem no `media_override` (R4). Promovido à doc 04
  (Override de relações).
- **MD5 — Ciclo de vida não-destrutivo; evento de arquivo só mexe a posse**
  (2026-07-08). Fecha a pergunta "arquivo some → vira wanted?": **não** — eventos de
  filesystem mexem apenas no eixo derivado da posse (MD2), nunca no `status`
  (intenção) nem no trabalho do dono. Arquivo sumido com datasource acessível vira
  `media_file` **`missing`** (soft, com `last_seen`), reversível se reaparecer;
  **datasource inteiro inacessível** não marca nada (evita falso sumiço em massa,
  sinaliza o datasource offline). Entity **auto-criada sem nenhum investimento do
  dono** cujo arquivo sumiu vira **órfã** numa fila de limpeza, nunca apagada
  automaticamente. **Purga** é a única operação destrutiva e é sempre explícita
  (remove entity + `media_external`/`media_override`/assets). Promovido à doc 04
  (Ciclo de vida e deleção) e doc 06 (missing/offline).
- **MD2 — Posse e `status` viram dois eixos ortogonais** (2026-07-07). Um campo só
  conflava "tenho o arquivo" (fato) com "o que pretendo" (intenção). Separado:
  **posse é derivada** da existência de `media_file` (não é campo editável, não
  diverge do disco, pode virar coluna gerada); **`status` é só intenção**, um estado
  por vez — `cataloged` (default), `wanted`, `unreleased`, `draft`. O valor `owned`
  sai (era posse disfarçada). Os eixos combinam livremente (`wanted`+posse=sim =
  tenho cópia mas quero upgrade), sustentando o "banco maior que a biblioteca". As
  transições de estado (arquivo aparece/some) ficam para o MD5. Promovido à doc 04.
- **MD4 — `external_ids` é a identidade autoritativa; `media_external` é cache**
  (2026-07-07). O id externo mora em dois lugares, então elege-se a autoridade:
  `entity.external_ids` é o **mapa de identidade** (todos os ids conhecidos, buscados
  ou não — é onde o match determinístico do MD1 grava e por onde o lookup busca).
  `media_external` é **cache do payload** por fonte buscada; seu `source_id` é
  subordinado, com o invariante `media_external.source_id == external_ids[source]`.
  Em conflito, a **identidade vence**: a linha de cache divergente é obsoleta
  (re-busca/descarta); o dono corrige a identidade e nunca edita o cache à mão.
  Identidade e valor editorial são eixos distintos — `media_external` permanece a
  camada base do resolver. Vale igual para `person`/`company`. Promovido à doc 04
  (seção "Identidade externa").
- **MD1 — Identidade e matching pela convenção Kodi/Jellyfin** (2026-07-07). Em vez
  de inventar regra própria, adota-se o padrão de-facto do ecossistema (Kodi/Jellyfin;
  Emby/Plex/\*arr na mesma família). Consequências: **grão 1:N plano** (1 obra = 1
  `Entity`, com N `media_file` para qualidades/edições/partes); **edição vira rótulo**
  no `media_file` (não duplica a obra); **partes** são empilhadas (*stacking*); a
  **chave de match** é o **id externo embutido no nome** (`[tmdbid-…]`/`{tmdb-…}`) →
  match determinístico, e sem id cai para título+ano. Todo match tem **score de
  confiança**: alta casa sem alerta; baixa casa com o melhor palpite **+ alerta de
  revisão** que o dono confirma/corrige; zero resultados → `draft` não-identificado.
  Música usa o mesmo princípio via MusicBrainz+tags. Verticais não cobertas
  (videoclipe→IMVDB, quadrinhos→ComicInfo) trazem identidade e provider próprios por
  `extension_data`, sem tocar no núcleo — validando R1/R5. Promovido para a doc 06
  (seção "Identidade e matching") e refletido no `media_file` da doc 04.
- **R1 — Espaço de campos particionado por origem** (2026-07-06). O resolver é um
  motor genérico que compõe camadas guiado por metadado por campo, não por uma
  tabela fixa no código. Origem declarada no schema (núcleo) ou no JSON Schema do
  namespace (extensão/plugin); só fontes-base são declaradas; override é universal.
  Consequência: adicionar vertical/plugin não toca o núcleo nem o resolver.
- **R2 — Prioridade entre fontes externas por lista global configurável**
  (2026-07-06). Ordem de preferência de fontes (default `[tmdb]`); por campo, a
  primeira fonte da lista que tiver valor forma a base externa, sobre a qual o
  override entra. Configurável pelo dono. Evoluções (por campo / fonte primária
  por entidade) ficam pós-MVP sem quebrar o modelo.
- **R3 — Override de listas por merge de operações** (2026-07-06). Para listas de
  valores simples (`tags`, `genres`), o override guarda adições e remoções por
  valor; a base externa continua viva e evolui em re-scrape. Só valores simples;
  override de relações (elenco/equipe) foi separado no item **MD6**.
- **R4 — Override como blob JSON esparso** (2026-07-06). `media_override` passa a
  ser uma linha por entidade com `override_json` espelhando o formato da entity,
  só campos mudados. Escolhido por casar com R1 (validação por schema), R3 (merge
  de listas) e R5 (override em extension_data). Substitui o esboço EAV da doc 04.
- **R5 — Composição uniforme, incluindo extension_data** (2026-07-06). O resolver
  não distingue núcleo de extensão: a mesma precedência vale campo a campo dentro
  dos namespaces. Decorre de R1 e R4.
- **R6 — Presença da chave sinaliza override** (2026-07-06). No blob esparso, chave
  presente (mesmo `null`) = override que vence; chave ausente = herda da base. UI
  distingue "limpar override" de "definir vazio". Proveniência por campo não é
  exposta na API por ora.
- **R7 — Fatos técnicos são overridáveis** (2026-07-06). Override universal alcança
  também mediainfo (corrigir detecção errada); o detectado original fica preservado
  em `media_file`. Sem exceção ao princípio de autoridade.
- **QG1 — Quality gate: catraca unificada Go + TS** (2026-07-08). Uma catraca
  (ratchet) única com um `baseline.json` cobrindo backend Go **e** frontend TS
  (Opção A entre unificado / dois gates / estender o boatman). Runner em **Node
  partindo do boatman** (gera a metade JS; estende-se o script para coletar métricas
  Go: golangci-lint, `go test -cover`, govulncheck, gofmt, gocyclo). Hooks locais com
  **lefthook** (poliglota) em vez de husky. Roda no `pre-push` local e no PR (bloqueia
  regressão + comenta). Promover os coletores Go ao boatman upstream fica como
  follow-up. Documentado em [ADR-0016](adr/0016-quality-gate-catraca-unificada.md) e
  no briefing [0005](../briefings/0005-quality-gate-catraca-unificada.md).
