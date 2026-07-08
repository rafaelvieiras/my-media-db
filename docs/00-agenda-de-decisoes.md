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
| **R6** | Como o dono "apaga" um valor — distinguir "não fiz override" de "override para vazio"? | **documentado** | **Presença da chave = override ativo**, mesmo com `null`/vazio (apaga o valor externo). Chave ausente = herda da base. A UI precisa separar "limpar override" (remove a chave) de "definir vazio" (chave com `null`). **Proveniência por campo não é exposta na API por ora** — a UI de edição consulta o override separadamente; expor proveniência fica como possível evolução (Fase 3). | `04` |
| **R7** | Fatos técnicos (mediainfo) são overridáveis pelo dono? | **documentado** | **Sim**, pelo mesmo mecanismo universal (R1) — sem regra especial. O dado técnico detectado permanece em `media_file` (não-destrutivo); o override entra por cima na composição. Uso raro (corrigir detecção errada do ffprobe), mas permitido. | `04` |

---

## Modelo de dados (fora do resolver, mas de fundação)

| ID | Questão | Status | Decisão | Destino |
|---|---|---|---|---|
| **MD1** | Identidade e matching: como um arquivo vira/associa a uma `Entity` e a um id externo? Um filme com múltiplos arquivos (edições/qualidades) — relação 1:N normativa. | **documentado** | Adota a **convenção Kodi/Jellyfin** (padrão de-facto): **1:N plano** (1 obra = 1 `Entity`; qualidades/edições/partes = `media_file`), **edição = rótulo no arquivo**, **id externo embutido no nome = chave determinística**, *stacking* de partes. Match carrega **score de confiança**: alta casa sozinho, baixa **casa mesmo assim + alerta de revisão** (*"mapeado com 60% de certeza. Remover?"*), zero resultados vira `draft` não-identificado; correção do dono é autoritativa (override). Verticais fora do padrão (videoclipe→IMVDB, quadrinhos→ComicInfo) trazem identidade própria via `extension_data`, sem tocar no núcleo. | `06` (+ `04`) |
| **MD2** | `status` (intenção do dono) vs posse (existe `media_file`?) estão conflados na doc 04. Separar em eixos ortogonais. | **documentado** | **Dois eixos ortogonais.** **Posse = fato derivado** (existe `media_file`?), não editável, não mente. **`status` = intenção do dono**, um estado por vez: `cataloged` (default), `wanted`, `unreleased`, `draft`. Removido o `owned` (era posse disfarçada). Combinam livre (ex.: `wanted`+posse=sim = quero upgrade tendo cópia). Transições entre estados → MD5. | `04` |
| **MD3** | Concorrência no SQLite: API sempre no ar enquanto o scan escreve pesado (single-writer). Estratégia (WAL + escritor único). | aberto | — | `03` |
| **MD4** | Identidade externa duplicada em `entity.external_ids` e `media_external.(source, source_id)`. Qual é a autoritativa? | **documentado** | **`entity.external_ids` é o mapa de identidade autoritativo** (todos os ids conhecidos, buscados ou não; é onde o match do MD1 grava e o lookup lê). **`media_external` é cache de payload**, `source_id` subordinado (invariante: bate com `external_ids[source]`). **Conflito → identidade vence**: linha divergente é cache obsoleto → re-busca/descarta; o dono corrige a identidade, nunca edita `media_external` à mão. Identidade ≠ valor editorial (media_external segue base do resolver). Mesma lógica p/ `person`/`company`. | `04` |
| **MD5** | Ciclo de vida / deleção: arquivo some do disco — a entity vira `wanted`? órfã? Comportamento não-destrutivo definido. | aberto | — | `04` |
| **MD6** | Override de **relações** (elenco/equipe): merge por `person_id`, edição de papel/ordem. Separado do R3 por ser bem mais complexo (tabelas `person`/`credit`). | aberto | — | `04` |

---

## Melhorias de base (estrutura da documentação)

| ID | Item | Status | Decisão | Destino |
|---|---|---|---|---|
| **ME1** | Adotar ADRs (um arquivo por decisão) em vez da lista achatada da doc 11. | aberto | — | novo `docs/adr/` |
| **ME2** | Diagrama ER (mermaid) do modelo de dados. | aberto | — | `04` |
| **ME3** | Criar `LICENSE` e `CONTRIBUTING.md` (inclinação MIT/Apache-2.0). | aberto | — | raiz |
| **ME4** | Glossário: termos de precedência do resolver e "posse" vs "status". | aberto | — | `02` |

---

## Histórico de decisões fechadas

<!-- Conforme cada item fecha, mover o resumo da decisão para cá com data. -->

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
