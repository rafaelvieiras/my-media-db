# 09 — NFO e export

> **Status desta doc:** stub escopado. Detalhamento em briefings da Fase 3.

## Princípios

- **Export é saída opcional, não a verdade.** A fonte de verdade é o banco. NFO
  (e outros formatos) são **projeções** do resultado do resolver, geradas quando
  o dono quer — nunca o mecanismo primário de armazenamento (é justamente o que
  diferencia do TinyMediaManager).
- **Não-destrutivo.** Gerar NFO/arte nas pastas é uma ação explícita e opcional.
- **Reflete o override.** O NFO exportado carrega o título/dados do dono.

## Escopo

- NFO no padrão **Kodi/Jellyfin/Emby/Plex** por tipo (`movie`, `tvshow`,
  `season`, `episode`, e verticais futuras).
- Alvo secundário: a **API `/api/tmdb`** ([05](05-api.md)) já cobre o consumo sem
  precisar escrever arquivos — o NFO é para quem prefere o fluxo clássico.

## Referência reaproveitável (v1)

A v1 tinha um modelo de NFO (music video) com **cada tag do Kodi documentada
inline** (obrigatoriedade, multiplicidade, notas). Esse rigor de documentar o
mapeamento tag-a-tag é o padrão a seguir para movie/tvshow/episode. Reescrever em
Go; usar a referência preservada em scratchpad como base do mapeamento.

## Decisões abertas (ver [11](11-decisoes-abertas.md))

- Motor de template vs serialização direta de struct→XML.
- Escrever arte junto do NFO ou só o NFO.
