# 0015 — Enriquecimento (import explícito) + proxy/cache de imagem

> O **import explícito**: puxar o detalhe de um resultado externo e persisti-lo como
> camada `media_external` (base editorial), gravar a identidade em `external_ids`, e
> **cachear as imagens em disco** — sem tocar no override.

| | |
|---|---|
| **ID** | 0015 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Docs relacionadas** | [07-enriquecimento-providers](../docs/07-enriquecimento-providers.md), [08-artwork-e-assets](../docs/08-artwork-e-assets.md), [ADR-0011](../docs/adr/0011-external-ids-autoritativo-media-external-cache.md), [ADR-0012](../docs/adr/0012-ciclo-de-vida-nao-destrutivo.md) |
| **Status** | draft |
| **Depende de** | [0014](0014-provider-tmdb-busca-local-first.md) |

---

## 1. Objetivo

A busca ([0014](0014-provider-tmdb-busca-local-first.md)) acha; falta **puxar** e
**persistir** — o passo que enche a camada externa que o resolver ([0013](0013-resolver-tres-camadas.md))
compõe. Segue **pull > sync** e **import explícito** ([07](../docs/07-enriquecimento-providers.md)):
nada sincroniza sozinho; o dono decide o que salvar. É aqui que um `draft`
não-identificado ou um item pobre ganha título, sinopse e poster.

## 2. Jornada do usuário

1. Na busca, o dono escolhe um resultado do TMDB e clica **Importar**.
2. O sistema puxa o detalhe, grava a base externa e o item passa a exibir os dados
   ricos (via resolver) — poster incluso, servido do **cache local**.
3. Re-enriquecer depois atualiza só a base externa; o que o dono editou
   ([0016](0016-editor-override-ui.md)) permanece intacto (não-destrutivo).

## 3. Escopo

**Dentro:**
- **`POST /api/v1/enrich`** (ou `/entities/{id}/enrich`) — puxa o detalhe do provider
  e **grava `media_external`** (payload cru), atualizando `entity.external_ids`
  (identidade autoritativa, MD4/[ADR-0011](../docs/adr/0011-external-ids-autoritativo-media-external-cache.md)).
- **Mapeamento** do payload TMDB → campos da `Entity`/base externa (título, sinopse,
  data, gêneros…); **sem** elenco/equipe (MD6 adiado).
- **Proxy/cache de imagem** ([08](../docs/08-artwork-e-assets.md)): baixar poster/fanart
  sob demanda, gravar **binário em disco** e um `asset` (`origin=external`) apontando
  para ele; servir do cache depois (não depender da fonte).
- **Não-destrutivo** ([ADR-0012](../docs/adr/0012-ciclo-de-vida-nao-destrutivo.md)):
  re-enriquecer atualiza `media_external`; nunca mexe em `media_override` nem em
  artes `origin=user`.
- Testes: import grava external + external_ids; re-import não apaga override; cache
  de imagem grava em disco e serve o binário.

**Fora (explicitamente):**
- **Elenco/equipe** (person/credit, MD6) — briefing próprio.
- **Upload** de arte custom (`origin=user`) e a escolha de arte preferida — [0017](0017-arte-custom-upload-ui.md).
- **Modo `promote_draft`/`hidden_cache`** do passthrough `/api/tmdb` — Fase 4 ([ADR-0015](../docs/adr/0015-api-tmdb-passthrough.md)).

## 4. Contrato técnico

### API (`/api/v1`)
- **POST** para importar por `(source, source_id)` sobre uma entity existente (ou
  criar a partir de um resultado externo — decidir na implementação). `200/201`
  com a entity resolvida; erros no envelope padrão.

### Dados / modelo
- Grava `media_external` por `(entity_id, source)` (upsert, [0012](0012-schema-fase3-camadas-externo-override.md));
  atualiza `external_ids_json` (e as colunas geradas `external_id_tmdb/imdb`).
- `asset` `origin=external` para cada imagem cacheada; binário no layout de cache
  em disco ([08](../docs/08-artwork-e-assets.md) — layout a definir aqui).

### Backend (Go)
- Usa o `Provider` do [0014](0014-provider-tmdb-busca-local-first.md) para o detalhe;
  escreve pelo pool de escrita, em lotes curtos (prioridade do dono, [ADR-0010](../docs/adr/0010-concorrencia-sqlite-wal-escritor-unico.md)).
- Download de imagem com timeout; falha degrada (item sem poster, não quebra o import).

### Frontend (Vite/React)
- Botão **Importar** no resultado externo da busca; estado de progresso/erro; o grid
  passa a mostrar o poster cacheado.

### Arquivos afetados
- Criar: handler `/enrich`, mapeador TMDB→entity, serviço de cache de imagem, testes.
- Alterar: `openapi.yaml` (+ regen tipos), UI de busca ([0014](0014-provider-tmdb-busca-local-first.md)) para disparar o import.

## 5. Critérios de aceite

- [ ] Importar um resultado grava `media_external` e atualiza `external_ids`; o resolver passa a servir os dados ricos.
- [ ] Poster/fanart são baixados **uma vez**, cacheados em disco como `asset` `origin=external`, e servidos do cache depois.
- [ ] Re-enriquecer atualiza a base externa e **não** apaga `media_override` nem artes `origin=user`.
- [ ] Falha da fonte/imagem degrada (import parcial claro), sem quebrar. Catraca verde.

## 6. Notas de implementação / decisões

- **Import escreve external, nunca override** — a separação de camadas é o que
  garante A3/B3/G2 ([04](../docs/04-modelo-de-dados.md)): o dono não perde edição ao
  re-scrapear.
- **Identidade vence** (MD4): o import grava/corrige `external_ids`; `media_external`
  é cache subordinado. Divergência → re-busca sob o id correto.
- **Layout do cache de assets** é decisão aberta da [08](../docs/08-artwork-e-assets.md) — fechar aqui o mínimo (diretório + nomeação estável); thumbnails/derivadas ficam fora.

## 7. Fora deste briefing / follow-ups

- Elenco/equipe (MD6); geração de thumbnails/derivadas; universal scraper; o
  passthrough `/api/tmdb` com `promote_draft`/`hidden_cache` (Fase 4).
