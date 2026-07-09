# 0013 — Resolver de 3 camadas (`override ⟵ externo ⟵ arquivo`)

> O coração do sistema: promover o resolver de **uma** camada (arquivo, [0010](0010-api-leitura-resolver-minimo.md))
> para a composição completa das três camadas, guiado por metadado de **origem por
> campo** — sem que a assinatura pública mude.

| | |
|---|---|
| **ID** | 0013 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Docs relacionadas** | [04-modelo-de-dados](../docs/04-modelo-de-dados.md), [ADR-0001](../docs/adr/0001-resolver-particionamento-por-origem.md)…[ADR-0007](../docs/adr/0007-fatos-tecnicos-sao-overridaveis.md) (R1–R7) |
| **Status** | done |
| **Depende de** | [0012](0012-schema-fase3-camadas-externo-override.md) |

---

## 1. Objetivo

Toda a promessa do modelo ([04](../docs/04-modelo-de-dados.md)) é: *a verdade
servida é a composta, não a bruta*. O 0010 entregou a **forma** da resposta com só
a camada arquivo; agora que as camadas `externo`/`override` existem ([0012](0012-schema-fase3-camadas-externo-override.md)),
o resolver passa a **compô-las de verdade** — e é isso que faz o override do dono e
o enriquecimento externo aparecerem em **toda** leitura, sem a UI mudar.

## 2. Jornada do usuário

Indireta mas central: quando o dono sobrepõe um título ([0016](0016-editor-override-ui.md))
ou importa dados do TMDB ([0015](0015-enriquecimento-import-proxy-imagem.md)), o
`/entities` e o `/entities/{id}` passam a devolver o **valor composto** (override
vence externo, que vence arquivo) — em toda a UI, sem tela especial.

## 3. Escopo

**Dentro:**
- **Motor genérico** ([ADR-0001](../docs/adr/0001-resolver-particionamento-por-origem.md)):
  compõe campo a campo guiado por **metadado de origem** (`arquivo`/`externo`/`nenhuma`),
  **não** por nome de campo hard-coded. Núcleo declara a origem no seu schema.
- **Precedência** `override ⟵ externo ⟵ arquivo` (R1) sobre os **campos editoriais da
  entity**. O override é universal por princípio (R7), mas o override de **fatos
  técnicos** (mediainfo) surge no endpoint `/files`, não na composição da entity —
  fica como follow-up (§7).
- **Presença da chave = override** ([ADR-0006](../docs/adr/0006-presenca-da-chave-sinaliza-override.md)):
  chave presente (mesmo `null`) vence; ausente herda a base.
- **Merge de listas por operações** para `tags`/`genres` (R3, [ADR-0003](../docs/adr/0003-override-de-listas-por-operacoes.md)):
  `{add, remove}` sobre a base externa, com dedup e ordem definida.
- **Prioridade entre fontes** por lista global configurável (R2, [ADR-0002](../docs/adr/0002-prioridade-entre-fontes-externas.md));
  default `[tmdb]` — a primeira fonte com valor forma a base externa.
- **Composição uniforme em `extension_data`** (R5, [ADR-0005](../docs/adr/0005-composicao-uniforme-extension-data.md)).
- Testes de composição por camada, presença-de-chave, merge de listas, prioridade
  e degradação (camadas ausentes = comportamento do 0010 preservado).

**Fora (explicitamente):**
- Override de **relações** (elenco/equipe, MD6) — briefing próprio (§7 do [0012](0012-schema-fase3-camadas-externo-override.md)).
- **Escrever** override/externo (é o [0015](0015-enriquecimento-import-proxy-imagem.md)/[0016](0016-editor-override-ui.md)); aqui só se **lê e compõe**.
- **Proveniência por campo** exposta na API (R6: não se expõe por ora).

## 4. Contrato técnico

### Backend (Go)
- `internal/resolver`: manter a **assinatura pública** `Resolver(Camadas) Entidade`
  (a UI não muda), trocando o interior por composição das três camadas. Ativa os
  campos `Externo`/`Override` já esboçados no 0010.
- **Camadas passa a carregar** (o chamador — handler de leitura — as busca via 0012):
  - `Externo []CamadaExterna` — **uma por fonte** com valor, já na forma **canônica**
    (`Fonte string` + `Campos map[string]any` com **os nomes dos nossos campos**, não
    o shape cru do provider). O mapeamento raw→canônico é do provider (0014/0015);
    o resolver **não conhece o TMDB**. Ordenadas pela **lista de prioridade** (R2;
    default `[tmdb]`).
  - `Override map[string]json.RawMessage` — o patch esparso cru de `media_override`
    (presença da chave = override, R6).
- **Origem por campo** = **registro declarativo** (dado, não `switch`): mapa
  `campo → {base: arquivo|externo|nenhuma, kind: escalar|lista|extensao}`. O motor
  **itera o registro** e compõe; nenhum ramo por nome de campo (R1). Fica fora do
  registro (não passa pela composição de camadas): `id`/`type`/`created_at`/
  `updated_at` (sistema), `status` (intenção do dono), `owned`/`file_count`
  (derivados), `poster_url` (link de API). `external_ids` segue vindo do núcleo
  (identidade autoritativa, MD4) — não é campo editorial de camada.
- **Composição por campo:** override (chave presente, mesmo `null`) ⟶ senão externo
  (primeira fonte da prioridade com valor) ⟶ senão arquivo (valor atual da Entity).
  Listas (`tags`) fazem **merge por operações** `{add, remove}` sobre a base externa
  (R3): ordem = base, remoções aplicadas, adições ao fim, dedup. `extension_data`
  compõe **por namespace/chave** com a mesma precedência (R5).
- Leitura das três camadas pelo pool de leitura; carga única por entity (evitar N+1).
- **Fiação nesta entrega = o detalhe** (`getEntity`, `internal/server/entities.go`):
  carrega `media_override` (é o patch cru, direto) e passa `Override` — composição de
  override **ponta a ponta**, testável via API. A **camada externa** fica completa e
  testada **no motor** (unit, camadas injetadas); sua fiação no handler entra no 0015,
  junto do provider que faz o `raw→canônico` (sem mapper agora, ligar `media_external`
  aqui seria código morto). A **lista/grid** usa query de card e não compõe aqui —
  nada grava externo/override até 0015/0016 (§7). Evita batch/N+1 prematuro.

### API
- Sem rotas novas: `/api/v1/entities` e `/{id}` já servem pelo resolver; muda o
  **conteúdo** quando há camadas. A forma é preservada, com **uma adição**:
  `extension_data` (objeto, default `{}`) passa a ser surfacado no shape resolvido —
  campo de núcleo (doc 04) que o R5 compõe; sem isso a composição de `extension_data`
  seria código morto. Atualizar `openapi.yaml` + regenerar os tipos TS (o front só
  ignora o campo novo enquanto nenhuma vertical o consome).

### Arquivos afetados
- Alterar: `internal/resolver/*.go` (+ testes), queries de leitura de `media_external`/`media_override` (via [0012](0012-schema-fase3-camadas-externo-override.md)).

## 5. Critérios de aceite

- [ ] Com só arquivo, o resolver devolve exatamente o do 0010 (regressão zero).
- [ ] Externo presente sobrepõe arquivo campo a campo respeitando a origem declarada; override sobrepõe ambos.
- [ ] Chave presente com `null` **apaga** o valor da base (R6); chave ausente herda.
- [ ] `tags`/`genres` compõem por `{add, remove}` sobre a base externa, com dedup e ordem definida (R3).
- [ ] Com duas fontes, vence a primeira da lista de prioridade que tiver valor (R2).
- [ ] `extension_data` compõe pela mesma precedência (R5). Catraca verde.

## 6. Notas de implementação / decisões

- **Assinatura estável é a chave** (nota do 0010): a Fase 3 preenche camadas por
  baixo; a UI não sente. Por isso o 0010 já entregou a forma final.
- **Motor genérico, não `switch` por campo** (R1): adicionar vertical/plugin não
  toca o resolver — ele só lê a etiqueta de origem.
- **Metadado de origem = registro declarativo** (decidido): mapa `campo → {base, kind}`
  que o motor itera. O motor **não** conhece campos por nome — trocar o registro (não
  o código) adiciona/muda campos. Alternativas (tags de struct via reflection, schema
  externo) descartadas por peso; o mapa é o suficiente e o mais legível para o MVP.
- **Externo canônico, não cru** (decidido): o resolver compõe sobre `map[string]any`
  com **os nossos** nomes de campo — o provider (0014/0015) faz o `raw→canônico`. Isso
  mantém o resolver genérico e **desacoplado do TMDB**, e deixa o 0013 testável **sem
  rede** (os testes injetam camadas externas/override direto). Que `media_external`
  guarde cru e/ou canônico é decisão do 0015 (quem grava); o 0013 só consome o canônico.
- **Fora da composição de camadas**, de propósito: `status` (intenção, não editorial),
  posse/contagem (derivados), `external_ids` (identidade, MD4), campos de sistema.

## 7. Fora deste briefing / follow-ups

- **Composição do card na lista/grid** (título/campos overridados no grid) — entra
  quando houver dado a compor (junto de 0015/0016), com **carga em lote** dos ids da
  página para não cair em N+1. Até lá o grid é correto (não existe override gravado).
- **Override de fatos técnicos (R7)** no endpoint `/files` — mesmo blob de override,
  surge onde o mediainfo é servido; discreto e de uso raro, fica para briefing próprio.
- Override de créditos (MD6); prioridade por campo / fonte primária por entidade
  (evolução pós-MVP do R2); expor proveniência por campo (possível Fase 4).
