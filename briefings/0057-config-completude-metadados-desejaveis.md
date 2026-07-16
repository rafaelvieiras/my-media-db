# 0057 — Config de completude de catalogação (admin, por tipo de schema)

> Tela de administração onde o dono define, POR TIPO de mídia, quais campos e
> artes são "desejáveis" para considerar um item devidamente catalogado.

| | |
|---|---|
| **ID** | 0057 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Docs relacionadas** | [04-modelo-de-dados](../docs/04-modelo-de-dados.md), [05-api](../docs/05-api.md), [08-artwork-e-assets](../docs/08-artwork-e-assets.md) |
| **Status** | done |
| **Depende de** | — (usa dados já existentes de [0029](0029-md6-elenco-equipe-person-credit-company.md) (elenco), [0044](0044-fluxo-de-artes-redondo.md)/[0048](0048-fundacao-providers-de-enriquecimento-arte-trailer-tema.md) (kinds de arte) e [0019](0019-fundacao-multi-idioma-settings-resolver.md) (padrão `app_settings`), mas não bloqueia neles — são pré-requisitos de DADOS, já entregues) |

---

## 1. Objetivo

O acervo cresce por import automático (scanner + provider) e nem todo item chega
com o mesmo nível de riqueza: um filme pode ter sinopse mas nenhuma arte além do
poster; uma série pode ter elenco mas nenhuma classificação indicativa. Hoje não
há como o dono expressar "o que eu considero um item bem catalogado" — e sem essa
régua, não dá para sinalizar de forma consistente quais itens merecem atenção
(esse sinal visual é o [0058](#7-fora-deste-briefing--follow-ups), que **consome**
o que este briefing entrega).

Este briefing entrega só a **config**: uma tela de administração onde o dono
marca, **para cada tipo de mídia do esquema** (hoje `movie` e `tv` — a
arquitetura já é multi-tipo por design, ver `AGENTS.md` §3), quais campos e
quais artes são desejáveis. A régua é por tipo porque os tipos têm naturezas
diferentes (uma série tem `airing_status`, um filme não; ambos têm sinopse).
Comparável à funcionalidade de "campos desejados"/"mídia completa" do
tinyMediaManager, mas adaptada ao modelo deste projeto (tipos livres, sem
tabela nova por tipo).

**Distinção crítica de nomenclatura (ver §6):** este é um conceito NOVO,
**completude de CATALOGAÇÃO** (preenchimento de metadados/artes de UM item).
O projeto já tem "completude" na UI (`web/src/lib/entity-badges.ts`,
`estadoCompletude`/`temCompletude`/`unidadeCompletude`, briefings 0036/0037) —
mas aquilo é **completude de POSSE**: quantos episódios/temporadas o dono TEM
no disco vs quantos existem catalogados na árvore. São selos **diferentes** que
podem coexistir no mesmo card. Este briefing e seus consumidores (0058/0059)
NÃO devem reusar `entity-badges.ts` nem o termo `completude` desacompanhado —
ver a convenção de nomes fixada no §6.

## 2. Jornada do usuário

1. O dono abre **Configurações** (`/settings`) e vê uma nova seção "Completude
   de catalogação", abaixo da seção de idiomas já existente.
2. Para cada tipo (**Filme**, **Série**), vê três grupos de checkboxes:
   - **Informações básicas**: sinopse, duração, classificação indicativa,
     palavras-chave.
   - **Elenco**: ter ao menos 1 crédito de elenco.
   - **Arquivo**: ter ao menos um arquivo de mídia presente na biblioteca.
   - **Artes** (um checkbox por kind): poster, fanart (background), logo,
     clearart, banner, thumb, disc (capa de disco).
3. No primeiro acesso (nunca configurado), o formulário já vem pré-marcado com
   o **default de fábrica**: sinopse + poster + arquivo de mídia, para os dois
   tipos — o resto desmarcado (ver §6, por que este default e não "tudo
   marcado").
4. O dono marca/desmarca o que quiser por tipo (ex.: exige elenco em `tv` mas
   não em `movie`) e clica Salvar.
5. O servidor valida (kinds de arte reconhecidos, tipos não-vazios) e grava.
   A tela confirma "Configurações salvas." (mesmo padrão inline da seção de
   idiomas — sem toast).
6. Daqui para frente, qualquer consumidor (o selo do 0058, o job do 0056) lê
   esta config para saber o que checar em cada item.

Desvio: se o dono nunca salvar nada, a leitura cai no default de fábrica do
passo 3 — não existe estado "sem config" que quebre um consumidor.

## 3. Escopo

**Dentro:**
- Nova chave em `app_settings` (reaproveita a tabela genérica do 0019 — **sem
  migração de schema nova**, ver §6): `catalog_completeness`.
- Endpoints **GET/PUT `/api/v1/settings/catalog-completeness`**: devolvem/gravam
  a config completa (todos os tipos configurados), com validação.
- Tela de admin: nova seção dentro da rota `/settings` já existente (mesma
  tela de idiomas, seção adicional — ver §6 sobre não criar rota nova),
  formulário com checkboxes agrupados por categoria, por tipo.
- Default de fábrica quando a chave não existe: `description` + `media_file` +
  arte `poster` marcados para `movie` e `tv`; todo o resto desmarcado.
- Helper Go para leitura em outros pacotes (cache leve em memória, mesmo
  padrão de `obterLanguageSettings`), para o 0058/0056 consumirem sem golpe
  de I/O por request.

**Fora (explicitamente):**
- **Cálculo do selo de completude por item e exibição na listagem** — isso é
  o [0058](0058-indicador-visual-item-incompleto-listagem.md) (nome
  hipotético; consome este briefing).
- **`trailer` como campo configurável** — o usuário mencionou que vem depois;
  citado aqui só como candidato futuro (§7). Não implementar agora.
- **Download/enriquecimento automático do que falta** — isso é o
  [0059](0059-grupo-botoes-acao-item.md) (nome hipotético).
- Granularidade por item individual (override "não exigir X neste item
  específico") — não pedido; se surgir, é follow-up.
- Novos kinds de arte (ex.: algo além do que já existe no registro
  `kindsValidos` de `internal/server/assets.go`) — fora de escopo; usa os
  kinds já implementados (ver §6, nota sobre `discoart`).
- Tipos além de `movie`/`tv` (ex. `season`, `episode`, `collection`, ou
  verticais futuras como música) — a config é **por design aberta a
  qualquer `type`** (ver §4), mas a SEED/UI deste briefing só popula
  `movie`/`tv`, que são os únicos tipos catalogáveis de topo com metadados
  ricos hoje.

## 4. Contrato técnico

### Dados / modelo

**Sem migração de schema nova.** Reaproveita `app_settings(key, value_json,
updated_at)` (migração 0005, briefing 0019) — o mesmo espírito do "esquema
extensível para plugins": uma config nova é só uma chave nova, não uma tabela
nova. Nova chave: `catalog_completeness`.

Shape do `value_json` (JSON canônico, chave de topo = `entity.type`, **aberto**
— qualquer string de tipo é aceita, não só `movie`/`tv`):

```json
{
  "movie": {
    "description": true,
    "runtime": false,
    "certification": false,
    "keywords": false,
    "cast": false,
    "media_file": true,
    "artwork": ["poster"]
  },
  "tv": {
    "description": true,
    "runtime": false,
    "certification": false,
    "keywords": false,
    "cast": false,
    "media_file": true,
    "artwork": ["poster"]
  }
}
```

Campos por tipo:
- `description` (bool) — sinopse. Espelha `Entidade.Description` (json
  `description`).
- `runtime` (bool) — duração. Espelha `Entidade.Runtime` (json `runtime`).
- `certification` (bool) — classificação indicativa. Espelha
  `Entidade.Certification` (json `certification`).
- `keywords` (bool) — ao menos 1 palavra-chave. Espelha `Entidade.Keywords`
  (json `keywords`, não confundir com `Tags`/gêneros).
- `cast` (bool) — ao menos 1 crédito de elenco. Espelha `Entidade.Cast` (json
  `cast`, briefing 0029/MD6).
- `media_file` (bool) — ao menos um `media_file` com `state='present'` para a
  entity. Espelha `Entidade.Owned`/`FileCount` (posse SIMPLES do item, não
  confundir com a posse AGREGADA de série/temporada do `Ownership`/0036).
- `artwork` (`[]string`) — lista de **kinds de arte** desejados, cada um
  marcável independentemente. Valida contra o registro existente
  `kindsValidos` de `internal/server/assets.go` — **não inventar kinds
  novos**. Ver §6 sobre `disc` (não `discoart`) e `characterart`/`still` fora
  do conjunto ofertado nesta tela.

Uma entrada de tipo ausente no JSON (ex.: dono nunca configurou `collection`)
significa "nada exigido" para aquele tipo — nunca marca incompleto.

### API

- **`GET /api/v1/settings/catalog-completeness`** — devolve a config vigente
  (ou o default de fábrica quando a chave não existe em `app_settings`, sem
  gravar nada — mesmo comportamento não-destrutivo de `GET /settings/languages`).
  `200` com o shape acima.
- **`PUT /api/v1/settings/catalog-completeness`** — corpo = o shape acima
  (substituição total, mesmo padrão do PUT de idiomas — não é PATCH parcial).
  Validação:
  - corpo não é objeto JSON válido → `400 invalid_body`.
  - alguma chave de tipo vazia (`""`) → `422 invalid_body`.
  - algum item de `artwork` fora do registro `kindsValidos` → `422
    invalid_body` (mensagem lista os kinds aceitos).
  - tipos sem nenhuma entrada (objeto vazio `{}`) são aceitos (equivale a
    "nada exigido" para os tipos ausentes).
  `200` com a config gravada (eco, mesmo padrão do PUT de idiomas).
- Erros seguem o padrão `{error, message}` do restante da API
  (`writeError`/`invalid_body`).

### Backend (Go)

- **`internal/server/catalog_completeness.go`** (novo arquivo, espelha
  `internal/server/settings.go`):
  - `type catalogCompletenessSettings map[string]tipoCompletenessConfig`
  - `type tipoCompletenessConfig struct { Description, Runtime,
    Certification, Keywords, Cast, MediaFile bool; Artwork []string }`
    (tags json snake_case correspondentes).
  - `defaultCatalogCompletenessSettings()` — `movie` e `tv` com
    `description=true, media_file=true, artwork=["poster"]`, resto `false`.
  - Cache em memória (`sync.Mutex`, `carregado bool`) igual a
    `settingsCache`/`obterLanguageSettings`; invalidado no PUT.
  - `(s *Server) obterCatalogCompletenessSettings(ctx) (catalogCompletenessSettings,
    error)` — helper REUTILIZÁVEL por outros pacotes/handlers (0056/0058 vão
    chamá-lo; não expor só via HTTP).
  - `valida()` no tipo de config: cada `artwork[i]` ∈ `kindsValidos` (chave
    exportada ou uma função exportada `KindsValidos()`/similar em
    `internal/server` para não duplicar o registro — decidir na implementação
    se vale mover `kindsValidos` para um pacote compartilhado, já que 0058
    também vai precisar dele fora de `internal/server` SE morar em pacote
    separado; se 0058 também for `internal/server`, não há problema de
    visibilidade).
  - `getCatalogCompletenessSettings`/`putCatalogCompletenessSettings`
    handlers, mesmo formato de `getLanguageSettings`/`putLanguageSettings`.
- **`internal/server/server.go`**: registrar rotas dentro do grupo
  `/settings` já existente (ao lado de `cfg.Get("/languages", ...)`):
  `cfg.Get("/catalog-completeness", s.getCatalogCompletenessSettings)` e
  `cfg.Put("/catalog-completeness", s.putCatalogCompletenessSettings)`.
- **`openapi.yaml`**: novo path `/api/v1/settings/catalog-completeness` (GET/
  PUT) + schema `CatalogCompletenessSettings` (`additionalProperties` do tipo
  `TypeCompletenessConfig` — objeto aberto por tipo, não enum fixo de
  `movie`/`tv`) + regenerar tipos TS (`gen:types`) e rodar `tsc -b` (catraca
  bloqueia erro de type-check web).

### Frontend (Vite/React)

- **`web/src/routes/settings.tsx`**: adicionar uma segunda seção/`<section>`
  abaixo do formulário de idiomas existente (mesma rota `/settings`, não uma
  rota nova — ver §6). Título "Completude de catalogação".
- Novo componente, ex. `web/src/components/catalog-completeness-form.tsx`:
  - Query (`catalogCompletenessSettingsQueryOptions` em `web/src/lib/queries.ts`,
    espelhando `languageSettingsQueryOptions`).
  - Mutation PUT (mesmo padrão `useMutation` + invalidação de
    `['settings', 'catalog-completeness']` — e, se o 0058 vier a depender de
    cache de listagem, invalidar `['entities']` também).
  - Formulário com um bloco por tipo (`Filme`, `Série` — rótulos pt-BR fixos
    para os dois tipos conhecidos hoje; **não** precisa descobrir tipos
    dinamicamente nesta rodada, já que só `movie`/`tv` têm UI de catalogação
    hoje), cada bloco com 3 `<fieldset>`: "Informações", "Elenco + arquivo",
    "Artes" (checkboxes com rótulo pt-BR por kind: Poster, Fanart, Logo,
    Clearart, Banner, Thumb, Capa de disco).
  - Validação client-side espelhando o backend (tipos não-vazios sempre
    presentes no form; nenhuma validação extra necessária além do schema já
    ser fixo no form).
  - Estado de sucesso/erro inline, igual ao formulário de idiomas (sem
    toast).
- **`web/src/lib/api.ts`**: `type CatalogCompletenessSettings =
  components['schemas']['CatalogCompletenessSettings']`,
  `getCatalogCompletenessSettings()`, `putCatalogCompletenessSettings(body)` —
  espelhando `getLanguageSettings`/`putLanguageSettings`.

### Arquivos afetados

- Criar: `internal/server/catalog_completeness.go` (+ teste
  `catalog_completeness_test.go`), `web/src/components/catalog-completeness-form.tsx`
  (+ teste).
- Alterar: `internal/server/server.go` (rotas), `openapi.yaml` (+ tipos
  gerados), `web/src/routes/settings.tsx` (nova seção), `web/src/lib/api.ts`,
  `web/src/lib/queries.ts`.
- **Não** cria migração nova em `internal/db/migrations/` (reaproveita
  `app_settings`).

## 5. Critérios de aceite

- [ ] `GET /settings/catalog-completeness` sem config prévia devolve o
      default de fábrica (`description`+`media_file`+`artwork:[poster]` para
      `movie` e `tv`) sem gravar nada em `app_settings`.
- [ ] `PUT /settings/catalog-completeness` com um `artwork` contendo um kind
      fora de `kindsValidos` → `422`.
- [ ] `PUT` com uma chave de tipo vazia (`""`) → `422`.
- [ ] `PUT` válido grava e o `GET` seguinte reflete exatamente o que foi
      enviado (substituição total, não merge parcial).
- [ ] A tela `/settings` mostra a nova seção "Completude de catalogação" com
      os dois tipos (Filme/Série), os checkboxes corretos, pré-marcados com o
      default no primeiro acesso.
- [ ] Salvar no formulário grava via PUT e mostra confirmação inline.
- [ ] Helper Go de leitura (`obterCatalogCompletenessSettings`) é chamável de
      fora do handler HTTP (é o que 0056/0058 vão importar).
- [ ] Catraca verde (inclui `tsc -b` do web após `gen:types`).

## 6. Notas de implementação / decisões

- **Nome do conceito — decisão travada para os briefings seguintes:**
  "**completude de catalogação**" (PT-BR, prosa/UI) / `CatalogCompleteness`
  (identificadores Go/API) / chave `catalog_completeness` (`app_settings` e
  rota). **Diferente** de "completude de posse" (`estadoCompletude` em
  `web/src/lib/entity-badges.ts`, briefings 0036/0037), que mede quantos
  episódios/temporadas o dono TEM no disco dentro de uma árvore de série. Os
  dois selos podem aparecer juntos no mesmo card (uma série pode estar
  "completa" em posse — tem todos os episódios — mas "incompleta" em
  catalogação — falta elenco). **0058/0059 não devem reusar
  `entity-badges.ts` nem os termos `EstadoCompletude`/`temCompletude`/
  `unidadeCompletude` para este conceito novo**; se precisarem de um helper
  puro análogo, criar um novo arquivo/módulo nomeado com o prefixo
  `catalog-completeness` (ou nome equivalente que não colida).
- **Zero migração de schema**: reaproveitar `app_settings` (chave/valor
  genérico, já pensado no 0019 exatamente para "futuras configs... sem nova
  migração") é mais alinhado ao princípio de esquema extensível do projeto do
  que criar uma tabela `completude_config(type, campo, obrigatorio)` por
  linha — que seria uma tabela-por-config, o análogo do "tabela de junção
  por-tipo" que o projeto evita. Um blob JSON por tipo, numa única chave, é
  suficiente: a config inteira é lida/escrita de uma vez (não há query
  incremental por campo) e o volume é irrisório (uma dezena de chaves por
  tipo, poucos tipos).
- **`kindsValidos` é a fonte única dos kinds de arte** (`internal/server/assets.go`,
  registro do briefing 0048): `poster`, `fanart`, `still`, `logo`, `clearart`,
  `disc`, `banner`, `thumb`, `characterart`. O pedido do usuário usou o nome
  "discoart" — **o sistema já tem esse conceito, mas com o nome `disc`** (não
  `discoart`); a tela de config deve oferecer `disc` com rótulo amigável
  "Capa de disco". `still` é usado hoje só para frame de EPISÓDIO (0042/0044)
  e não faz sentido como "desejável" de `movie`/`tv` de topo — **fora da
  oferta desta tela** (não remove do registro, só não aparece como checkbox
  aqui). `characterart` existe no registro (fanart.tv) mas não foi pedido
  pelo usuário — também fora da oferta desta tela; se algum dia quiserem
  incluí-lo, é só adicionar ao array de checkboxes do formulário (o backend
  já aceita, pois valida contra `kindsValidos` inteiro).
- **Default de fábrica deliberadamente mínimo** (sinopse + poster + arquivo):
  o objetivo é não fazer bibliotecas existentes "explodirem" em selos de
  incompleto assim que o 0058 for implementado. Um default "tudo marcado"
  puniria retroativamente todo acervo já catalogado sem o dono ter pedido
  nada. Sinopse/poster/arquivo são os três sinais mais básicos de "isto é um
  item real, catalogado" — equivalentes ao mínimo que o próprio scanner+match
  já garante na prática para um item não-draft.
- **Config por tipo aberta (não enum fixo)**: embora a UI só ofereça
  `movie`/`tv` nesta rodada, o schema (JSON e OpenAPI) não fixa um enum —
  fica coerente com `entity.type` sendo `TEXT` livre (sem `CHECK`, ver
  `internal/db/migrations/0008_library_source_content_type.sql`) e evita uma
  migração de schema/OpenAPI quando um tipo novo (música, etc.) precisar de
  config própria — só estende a UI.
- **Substituição total no PUT (não PATCH)**: mesmo padrão do
  `PUT /settings/languages` — o formulário sempre manda o objeto inteiro (os
  dois tipos), então não há ambiguidade sobre "campo omitido = false" vs
  "campo omitido = não mexer".
- **Sem tabela por-tipo, sem `CHECK` no banco**: a validação de tipo/kind vive
  inteiramente no handler Go (mesmo espírito do `value_json` sem validação de
  schema no SQLite, nota da migração 0005) — mantém `app_settings` genérica e
  a regra de negócio num único lugar (Go), não duplicada em SQL.
- **Alternativa descartada — rota `/settings` própria (`/settings/catalog-completeness`
  como tela separada)**: preferiu-se uma SEGUNDA SEÇÃO na mesma tela
  `/settings` já existente, seguindo o agrupamento "Administração" do
  `app-header.tsx` (que já reúne Configurações + Jobs como área de operação,
  não navegação do dia a dia) — menos uma rota para o dono descobrir, e o
  padrão de card/seção do TinyMediaManager (uma tela de "Preferências" com
  seções) é o análogo mais próximo do que o usuário pediu.

## 7. Fora deste briefing / follow-ups

- **[0058] Indicador visual de item incompleto na listagem** — calcula, por
  item, se os campos/artes marcados como desejáveis (lidos daqui) estão
  presentes, e renderiza o selo no card. Depende deste briefing.
- **[0059] Grupo de botões de ação por item** — pode usar a config daqui para
  sugerir ações (ex. "baixar poster que falta"), mas o download automático em
  si é escopo dele, não deste briefing.
- **[0056] Job de saúde tenta enriquecer via provider itens draft** — não
  depende diretamente deste briefing (atua em itens *sem match*, antes mesmo
  de ter metadados para avaliar completude), mas pode futuramente cruzar com
  esta config para decidir o que vale a pena tentar buscar primeiro.
- **`trailer` como campo desejável**: mencionado pelo usuário como algo que
  "vem depois" — não implementar agora. Quando entrar, é só mais um bool no
  `tipoCompletenessConfig` (ex. `trailer bool`) — o shape já é aditivo o
  suficiente para isso sem quebrar compatibilidade (campo novo com default
  `false`/ausente = não exigido).
- Granularidade por item (override individual "não exigir X neste item") —
  não pedido; se surgir demanda, outro briefing.
- Descoberta dinâmica de tipos na UI (hoje hardcoded a `movie`/`tv`) — só
  vale a pena quando houver um segundo tipo de topo com metadados ricos
  (ex. vertical de música).
