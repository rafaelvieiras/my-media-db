# 0040 — Seletor de ordenação de episódios (official/dvd/absoluto)

> Na tela da série, um seletor troca a ordem de exibição dos episódios. A ordem
> default mostra os cards de temporada navegáveis (como hoje); ordens alternativas
> reagrupam os episódios por "Temporada N (ORDEM)", projetados na leitura a partir do
> `numbering` — sem criar entidade de temporada por ordem. A ordem escolhida vive na
> URL (search param), não é persistida.

| | |
|---|---|
| **ID** | 0040 |
| **Tipo** | feature |
| **Status** | done |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override (vertical séries) |
| **Docs relacionadas** | [0039](0039-numbering-real-no-card-do-episodio.md), [0031](0031-series-fundacao-modelo-enriquecimento.md), [ADR-0021](../docs/adr/0021-modelo-series-tv.md) |
| **Depende de** | 0039 |

---

## 1. Objetivo

O modelo (ADR-0021) trata **ordenação como projeção pura**: o episódio é a entidade
canônica; trocar de ordem (official/dvd/absoluto) é **renumerar/reagrupar**, nunca mudar
granularidade. Os dados importados já carregam múltiplas ordens no `extension_data`
(`core.tv@1.numbering:{official,dvd,...}` + `absolute_number`) — e elas **divergem de
verdade** (na base atual, ~20% dos episódios têm DVD ≠ oficial). Hoje a UI só mostra a
ordem default (temporadas oficiais) e não há como ver a série em DVD ou absoluto. Este
briefing entrega o **seletor de ordenação** na tela da série.

**Restrição estrutural (ADR-0021):** temporadas de ordens alternativas **NÃO são
entidades** (decisão "ENXUTO": só a ordem default tem `season`-entity). Logo a ordem
alternativa é **agrupada na leitura, no server**, a partir do JSON — cabeçalhos de
temporada não são navegáveis; só o **episódio** (entidade real) é clicável.

## 2. Jornada do usuário

1. Na tela de uma **série**, um seletor de ordem aparece **apenas quando há mais de uma
   ordem disponível** (do contrário, nada muda). Começa na ordem **default** da série.
2. **Ordem default** → exatamente o de hoje: grid de **cards de temporada** navegáveis
   (abre a temporada → episódios, com o número real do 0039).
3. **Ordem alternativa** (ex.: DVD, Absoluto) → a série é reexibida como **seções por
   temporada da ordem** ("Temporada 1 (DVD)", cabeçalho não-clicável), cada uma listando
   os **episódios** (cards clicáveis) com o número **daquela** ordem (E01, E02…). No
   **absoluto**, uma lista única corrida (#001, #002…), sem temporadas.
4. A ordem escolhida fica na **URL** (`?ordering=dvd`) — compartilhável e volta ao
   default ao sair; não é lembrada entre visitas (decisão do dono: preferência efêmera).

## 3. Escopo

**Dentro:**
- **Descoberta de ordens** por série: server calcula `available_orderings` (união das
  chaves de `numbering` presentes nos episódios da série + `absolute` quando algum
  episódio tem `absolute_number`) e `default_ordering` (o `default_season_type` da série
  — hoje gravado em `enrich/serie.go:~273` mas **nunca lido**; passa a ser lido).
- **Endpoint de episódios agrupados**: `GET /api/v1/entities/{id}/episodes?ordering=X`
  devolve os episódios da série **agrupados e ordenados** pela ordem `X`, projetados do
  `numbering` (reusa a projeção do 0039). Cada episódio vem como card (mesmo `EntityCard`,
  com `numbering` já na ordem `X`). Grupos = temporadas da ordem; `absolute` = 1 grupo
  corrido por `absolute_number`.
- **Selector na UI** (tela da série): dropdown com as ordens disponíveis; muda o
  **search param** `ordering` da rota (TanStack Router). Default → view de temporadas
  atual; alternativa → view agrupada de episódios.

**Fora (explicitamente):**
- Persistir a ordem (localStorage/servidor) — é só URL/router state.
- Criar entidade de temporada para ordens alternativas (proibido pelo ADR).
- Reprojetar/mutar `sort_index` das relações — a ordem default segue intocada; a
  alternativa é **computada na leitura**, sem escrever nada.
- Trocar a ordem default da série (isso é override/edição — outro briefing).
- Ordens no card da home/busca (só na tela da série).

## 4. Contrato técnico

### Backend (Go)
- **Ordens disponíveis no detalhe da série**: anotar o DTO de detalhe de `tv`
  (`GET /entities/{id}`) com `orderings { default: string, available: []string }`
  (`omitempty`, só para `tv`). Anotação **no server** (precedente Ancestors/Ownership,
  0034/0036), não na query. `available` ordenado de forma estável com o default primeiro.
  - Cálculo: varrer os episódios descendentes da série (via relações série→temporada→
    episódio já existentes), coletar as chaves de `numbering` e a presença de
    `absolute_number`. Reusar o parser `internal/tvext` (0039). Se a série tiver só 1
    ordem, `available` tem 1 item (a UI então esconde o seletor).
- **Endpoint** `GET /entities/{id}/episodes?ordering=X`:
  - Handler novo (ex.: `getEntityEpisodes`), registrado junto de `/members`, `/files`.
  - Valida `ordering` contra `available_orderings` da série (fallback ao default se
    ausente/inválido). `absolute` é uma ordem válida especial.
  - Junta **todos os episódios** da série (descendentes), projeta cada um na ordem `X`
    (reusa a projeção do 0039: `numbering[X] → {season,number}`; para `absolute`, usa
    `absolute_number`), ordena por `(season, number)` (ou por `absolute_number`), e
    **agrupa** por `season`. Resposta:
    ```json
    { "ordering": "dvd",
      "groups": [ { "season": 1, "label": "Temporada 1",
                    "episodes": [ /* EntityCard com numbering já em dvd */ ] } ] }
    ```
    Para `absolute`: um único grupo (`season` ausente/0, `label` p.ex. "Episódios"),
    episódios ordenados por absoluto, `numbering` de cada card refletindo o absoluto.
  - Episódio sem entrada para a ordem `X` (draft/sem numbering) → cai num grupo
    "Sem numeração" ao final (ou é omitido — decidir na implementação, documentar).
  - **Perf**: varredura série→temporadas→episódios é N+1 nas relações. Para a escala
    atual (centenas de episódios/série) é aceitável; deixar `TODO` de query agregada
    única como no 0037/0039. Não otimizar prematuramente.
- **Reuso**: a projeção `numbering[ordering] → DTO` do 0039 (`projetarNumbering`/
  `internal/tvext`) deve ser reaproveitada pelos dois caminhos (members e episodes), sem
  duplicar. Refatorar para um helper comum se necessário (cuidado com gocyclo ≤10).

### Frontend (Vite/React, TanStack Router + Query)
- **Search param na rota** da série: adicionar `ordering` (opcional) à rota de detalhe de
  entity (validar com Zod: string; default = ausente). Ler via `Route.useSearch()`.
- **Seletor**: componente que aparece na tela da série **apenas se**
  `orderings.available.length > 1`. Opções = `orderings.available`, com rótulos
  amigáveis (`official`→"Oficial", `dvd`→"DVD", `absolute`→"Absoluto", demais →
  capitalizado). Trocar a opção faz `navigate({ search: { ordering } })` (ou remove o
  param quando volta ao default).
- **Renderização condicional na tela da série**:
  - `ordering` ausente **ou** == `orderings.default` → view atual (cards de temporada via
    `MembrosSomenteLeitura`).
  - `ordering` != default → buscar `entities/{id}/episodes?ordering=X` (nova query em
    `web/src/lib/queries.ts`) e renderizar **seções**: para cada grupo, um cabeçalho
    **não-navegável** ("Temporada N", ou "Episódios" no absoluto) + grid de `PosterCard`/
    `CardNumerado` dos episódios (reusa `GradeMembros`/`CardNumerado` do 0039). Estados de
    carregando/erro/vazio.
- **api.ts / tipos**: tipar o objeto `orderings` no detalhe e o retorno do endpoint de
  episódios (via `gen:types` a partir do OpenAPI).

### OpenAPI
- `openapi.yaml`: adicionar `orderings` ao schema de detalhe da entity (objeto
  `default`,`available`); adicionar o endpoint `GET /entities/{id}/episodes` com o param
  `ordering` e o schema de resposta (`EpisodeGroup`/`EpisodeOrdering`). Rodar `gen:types`;
  o gerado deve sair **já formatado** (Prettier roda da raiz). `tsc -b` zero.

### Arquivos afetados (previsão)
- Backend: `internal/server/entities.go` (DTO `orderings` + anotação), novo handler de
  episódios + registro de rota (`internal/server/server.go` ou onde as rotas de entity
  são montadas), possível helper compartilhado de projeção/varredura, reuso de
  `internal/tvext`. Sem migration.
- Frontend: rota de detalhe da entity (`web/src/routes/...` — search param), componente
  seletor, componente de view agrupada, `web/src/lib/queries.ts`, `web/src/lib/api.ts`,
  tipos gerados.
- `openapi.yaml`, testes (Go do handler + projeção; front do seletor + view agrupada +
  troca de search param).

## 5. Critérios de aceite

- [ ] `GET /entities/{tvId}` traz `orderings{default,available}` para `tv` (ausente p/
      outros tipos); `available` inclui `absolute` quando há `absolute_number`.
- [ ] `GET /entities/{tvId}/episodes?ordering=dvd` devolve os episódios agrupados por
      temporada da ordem DVD, com `numbering` de cada card já em DVD; `?ordering=absolute`
      devolve lista única por absoluto. `ordering` inválido cai no default.
- [ ] Tela da série: seletor aparece só com >1 ordem; default = cards de temporada
      (inalterado); alternativa = seções "Temporada N" não-navegáveis com episódios
      navegáveis; absoluto = lista corrida.
- [ ] A ordem escolhida está na URL (`?ordering=`), compartilhável; sair da tela não
      persiste a escolha.
- [ ] Nenhuma entidade de temporada nova é criada; nenhum `sort_index` é mutado
      (ordem default idêntica ao 0039).
- [ ] Catraca completa verde (Go cov, e2e, tsc 0, web cov, jscpd, gocyclo ≤10).
- [ ] Smoke real: numa série com DVD ≠ oficial, trocar para DVD reagrupa/renumera; trocar
      para Absoluto lista corrido; voltar ao default restaura os cards de temporada.

## 6. Notas de implementação / decisões

- **Projeção na leitura, sem entidade** (ADR-0021): ordens alternativas são derivadas do
  `numbering` no server e agrupadas na resposta; só a default tem `season`-entity
  navegável. Cabeçalhos alternativos são rótulos, não links.
- **Ordem na URL, sem persistência** (decisão do dono): é preferência de visualização
  efêmera; `?ordering=` no router basta (compartilhável, volta ao default). Sem
  localStorage nem schema.
- **Default finalmente lido**: `default_season_type` já era gravado no import mas nunca
  consumido; passa a alimentar `orderings.default` e a decidir a view.
- **Reuso do 0039**: a mesma projeção `numbering→{season,number}`/`absolute` alimenta o
  card de episódio no /members e o endpoint agrupado — um helper só.

## 7. Fora deste briefing / follow-ups

- Editar a ordem **default** da série (override do dono) — briefing futuro.
- i18n de título de episódio (**0041**) e still de episódio (**0042**).
- Query agregada única (perf) para varrer episódios da série, se a biblioteca crescer.
- Marcar posse/completude por ordem alternativa (hoje a completude é da árvore default).
