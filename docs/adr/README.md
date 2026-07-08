# Architecture Decision Records (ADRs)

Cada decisão de **fundação** já fechada vira um **ADR** — um arquivo curto,
imutável e datado. É a memória de *por que* o projeto é como é: substitui a lista
achatada de "decisões abertas" por um registro rastreável, um arquivo por decisão.

## Como funciona (formato leve)

- Um ADR por decisão, numerado em sequência: `NNNN-slug.md`.
- Segue o [template](template.md): **Contexto · Decisão · Consequências**, mais
  status, data e o **ID na agenda** ([`docs/00`](../00-agenda-de-decisoes.md)).
- **ADR é imutável.** Não se reescreve uma decisão: quando uma nova a supera, cria-se
  um **novo ADR** que a marca como `Substituída por ADR-XXXX`, e vice-versa.
- Fluxo: uma decisão nasce na [agenda](../00-agenda-de-decisoes.md) (`aberto` →
  `documentado`), vira ADR, e é promovida às docs de spec (`03`, `04`, …).

## Índice

| ADR | Decisão | Agenda | Status |
|---|---|---|---|
| [0001](0001-resolver-particionamento-por-origem.md) | Resolver: campos particionados por origem | R1 | Aceita |
| [0002](0002-prioridade-entre-fontes-externas.md) | Prioridade entre fontes externas (lista global) | R2 | Aceita |
| [0003](0003-override-de-listas-por-operacoes.md) | Override de listas por merge de operações | R3 | Aceita |
| [0004](0004-override-como-blob-json-esparso.md) | Override como blob JSON esparso | R4 | Aceita |
| [0005](0005-composicao-uniforme-extension-data.md) | Composição uniforme (inclui `extension_data`) | R5 | Aceita |
| [0006](0006-presenca-da-chave-sinaliza-override.md) | Presença da chave sinaliza override | R6 | Aceita |
| [0007](0007-fatos-tecnicos-sao-overridaveis.md) | Fatos técnicos são overridáveis | R7 | Aceita |
| [0008](0008-identidade-e-matching-kodi-jellyfin.md) | Identidade e matching pela convenção Kodi/Jellyfin | MD1 | Aceita |
| [0009](0009-posse-e-status-eixos-ortogonais.md) | Posse e `status` como eixos ortogonais | MD2 | Aceita |
| [0010](0010-concorrencia-sqlite-wal-escritor-unico.md) | Concorrência SQLite: WAL + escritor único | MD3 | Aceita |
| [0011](0011-external-ids-autoritativo-media-external-cache.md) | `external_ids` autoritativo, `media_external` cache | MD4 | Aceita |
| [0012](0012-ciclo-de-vida-nao-destrutivo.md) | Ciclo de vida não-destrutivo | MD5 | Aceita |
| [0013](0013-override-de-relacoes-por-operacoes.md) | Override de relações (elenco/equipe) por operações | MD6 | Aceita |
| [0014](0014-stack-de-implementacao.md) | Stack de implementação (chi, modernc, sqlc, ffprobe, OpenAPI) | doc 11 (1–5) | Aceita |
| [0015](0015-api-tmdb-passthrough.md) | `/api/tmdb`: passthrough com cache, nunca 404 | doc 11 (6) | Aceita |
| [0016](0016-quality-gate-catraca-unificada.md) | Quality gate: catraca unificada (ratchet) Go + TS | QG1 | Aceita |
