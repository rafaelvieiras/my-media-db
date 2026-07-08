# 08 — Artwork e assets

> **Status desta doc:** stub escopado. Detalhamento em briefings da Fase 3.

## Princípios

- **Binário em disco, metadado no banco.** Um `asset` guarda no SQLite apenas
  tipo, origem, caminho/URI e metadados; o arquivo em si vive em disco (nunca no
  JSON — ver [04](04-modelo-de-dados.md)).
- **Origem `external` ou `user`.** Artes puxadas de fontes e artes enviadas pelo
  dono coexistem.
- **A escolha do dono vence.** A arte marcada como preferida é a que a API serve
  por padrão — inclusive para o Jellyfin ([cenário B/G](01-visao-e-casos-de-uso.md)).
- **Upload sem aprovação.** O dono sobe a arte que quiser, sem moderação externa.
- **Não-destrutivo.** Re-enriquecer não remove artes custom.

## Tipos de asset (ponto de partida)

`poster`, `fanart/backdrop`, `banner`, `clearlogo`, `thumb`, `season poster`,
`episode thumb` — ampliar conforme verticais.

## Proxy e cache de imagem

Imagens externas são buscadas sob demanda e **cacheadas localmente** após o
primeiro acesso, evitando dependência contínua da fonte.

## Decisões abertas (ver [11](11-decisoes-abertas.md))

- Layout de diretórios do cache/assets em disco.
- Geração de thumbnails/derivadas e formatos.
