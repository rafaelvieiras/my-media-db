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
| **Status** | draft |
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
- **Precedência** `override ⟵ externo ⟵ arquivo` (R1); **override universal** (alcança
  qualquer campo, inclusive técnico — R7).
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
- `internal/resolver`: manter a **assinatura pública** do 0010 (a UI não muda),
  trocando o interior por composição das três camadas. Os pontos de extensão já
  comentados no 0010 (`Externo`/`Override`) são ativados aqui.
- **Origem por campo** como tabela de metadado do núcleo (pequena, estável); o motor
  a consulta para decidir de onde cada campo pode vir.
- Leitura das três camadas pelo pool de leitura; join/carga única (evitar N+1).

### API
- Sem rotas novas: `/api/v1/entities` e `/{id}` já servem pelo resolver; muda o
  **conteúdo** quando há camadas. `openapi.yaml` inalterado (mesma forma).

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
- Decidir na implementação a representação do metadado de origem (tags de struct,
  mapa declarativo, ou anotação no schema) — resultado obrigatório: o motor **não**
  conhece campos por nome.

## 7. Fora deste briefing / follow-ups

- Override de créditos (MD6); prioridade por campo / fonte primária por entidade
  (evolução pós-MVP do R2); expor proveniência por campo (possível Fase 4).
