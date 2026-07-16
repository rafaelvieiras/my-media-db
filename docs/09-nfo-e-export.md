# 09 — NFO e export

> **Status desta doc:** stub escopado. Detalhamento em briefings da Fase 4.

## Princípios

- **Export é saída opcional, não a verdade.** A fonte de verdade é o banco. NFO
  (e outros formatos) são **projeções** do resultado do resolver, geradas quando
  o dono quer — nunca o mecanismo primário de armazenamento (é justamente o que
  diferencia do TinyMediaManager).
- **Não-destrutivo.** Gerar NFO/arte nas pastas é uma ação explícita e opcional.
- **Reflete o override.** O NFO exportado carrega o título/dados do dono.
- **Preserva o que não é nosso.** Ao regravar um `.nfo` existente, qualquer
  elemento de nível superior que o MyMediaDB não gera/gerencia (tags do Kodi,
  `<fileinfo>`, edição manual, etc.) é reembutido tal como estava — só os
  campos que o MyMediaDB de fato modela (título, sinopse, elenco...) vêm
  sempre do banco (briefing 0065).
- **`<thumb>`/`<rating>` no formato que Kodi/Jellyfin realmente conseguem ler
  (briefing 0066).** Pôster/fundo de filme/série são sempre materializados no
  disco ANTES de gravar o `.nfo` (nunca apontam para a API interna do
  MyMediaDB) — o Kodi/Jellyfin descobrem a arte sozinhos pela convenção de
  nome de arquivo. `<actor><thumb>` usa a URL direta do provider (nunca a
  convenção `.actors` do Kodi, que o Jellyfin não suporta). `<rating>` sai no
  formato atual do Kodi, com o wrapper `<ratings>`.

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
