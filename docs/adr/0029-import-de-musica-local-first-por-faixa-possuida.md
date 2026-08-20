# ADR-0029 — Import de música local-first: só o que a faixa possuída trouxer

- **Status:** Aceita
- **Data:** 2026-08-19
- **Agenda:** [MU1–MU4](../00-agenda-de-decisoes.md#modelo-de-música--fundacional-levantada-a-partir-de-pedido-do-dono-pra-implementar-o-schema) · **Spec:** [ADR-0027](0027-modelo-de-musica-hierarquia-artista-album-faixa.md), [AGENTS.md](../../AGENTS.md) §4/§6

## Contexto

O ADR-0027/briefing 0106 decidiu, como trade-off aceito, que casar **uma**
faixa local contra o MusicBrainz dispara o import da **discografia inteira**
do artista (todos os álbuns, todas as faixas, como entities `cataloged` sem
posse). Em produção isso gerou uma distorção grave: 411 arquivos de áudio
reais no disco (confirmado via `find`) produziram **478 artistas, 236 álbuns e
2478 faixas** no banco — só 98 faixas realmente possuídas (`owned=true`).
O dono comparou contra o **LMS** (`epoupon/lms`, servidor Subsonic self-hosted
já rodando em produção sobre a mesma biblioteca, `/userdata/gdrive/Musicas`) e
confirmou o padrão de mercado: o LMS deriva artista/álbum/faixa **direto das
tags ID3 dos arquivos que existem**, sem sair buscando o catálogo externo
inteiro — resultado, no banco do próprio LMS (`lms.db`), é **153 artistas, 72
álbuns, 411 faixas**, 1:1 com os arquivos físicos.

O comportamento atual também tensiona o próprio [AGENTS.md](../../AGENTS.md)
§4: "Fontes externas são enriquecimento **opcional** e **puxado** (pull, nunca
sync automático)". Importar a árvore inteira a partir de 1 faixa casada
durante um scan passivo é, na prática, um sync automático não solicitado —
não um pull explícito do dono.

## Decisão

1. **Materialização de artista/álbum é local-first.** Só se cria `entity
   type=artista`/`type=album` quando o scanner efetivamente **casa um arquivo
   real** contra uma faixa. Nenhum import de árvore completa é disparado a
   partir do matching automático.
2. **Artista principal** de uma faixa possuída vem do artist-credit resolvido
   pela fonte externa para aquela faixa específica (fallback: tag local
   `AlbumArtist`/`Artist` quando a fonte não resolve) — nunca da enumeração de
   toda a discografia do artista.
3. **Álbum importa só a si mesmo.** Ao identificar a faixa possuída, importa-se
   o release (álbum) correspondente — não os demais álbuns do mesmo artista.
   As faixas do release que o dono **não** tem em disco não viram entity
   `cataloged` automaticamente e silenciosamente: ficam expostas como
   "faltando" na tela do álbum (mesma superfície de item externo
   ainda-não-importado usada por filme/coleção/série), e o dono decide
   individualmente marcar como **desejado** (`status='wanted'`, ADR-0009) —
   nunca em massa e nunca sem ação explícita.
4. **Artista secundário/feat não é auto-importado.** Quando o artist-credit da
   faixa diverge do artista do álbum (colaboração/feat), o sistema **não**
   cria entity+`entity_relation(kind=music_artista_feat)` automaticamente. A
   informação fica disponível (metadado) para o dono decidir importar
   explicitamente — comportamento padrão é "não criar".
5. **Import manual de artista continua disponível, intacto.** `POST
   /api/v1/import` com `type=artista` a partir de uma busca deliberada
   continua trazendo a árvore inteira (mesmo comportamento do briefing 0106) —
   isso é um **pull explícito** do dono, não o gatilho automático do scan.
   Ou seja: a capacidade de árvore completa do `MusicaProvider` não é
   removida, só deixa de ser invocada implicitamente pelo matching de arquivo.
6. `MusicaProvider` (MusicBrainz) ganha uma capacidade seletiva — buscar **um**
   release/álbum específico por MBID sem enumerar a discografia do artista —
   para servir o novo fluxo do scanner (o client já tem `montarAlbum` interno
   fazendo exatamente isso; falta expor como capacidade pública granular).

## Consequências

- Corrige a distorção real de escala achada em produção sem abrir mão do
  enriquecimento: o dono ainda importa uma discografia inteira quando quiser,
  via busca manual — só deixa de acontecer nas costas dele a partir de 1
  arquivo.
- Alinha o comportamento ao princípio §4 do AGENTS.md (pull explícito, nunca
  sync automático) — o briefing 0106 já citava esse princípio, mas a
  implementação do trigger automático o contrariou na prática; este ADR é a
  correção de rota, não uma reversão do modelo de dados (ADR-0027 continua
  valendo integralmente: `entity`/`entity_relation`, kinds, `extension_data
  core.music@1` — nada disso muda).
- Reaproveita o padrão "banco maior que a biblioteca" (item de faixa faltante
  ↔ episódio/item não possuído) em vez de inventar um mecanismo novo de
  "wanted" para música.
- Fica **fora de escopo** (achado, não resolvido aqui): série/coleção têm o
  mesmo padrão de "importa a árvore inteira" (`importarSerie` traz todas as
  temporadas/episódios de uma vez) — tensão análoga, mas não mexida por este
  ADR; decisão futura se o dono quiser estender o mesmo racional.
- Exige uma limpeza de dado em produção (remover artista/álbum/faixa
  catalogados sem posse que resultaram do comportamento anterior) — ação
  operacional feita **depois** do deploy do novo comportamento, para não
  reintroduzir o problema num scan subsequente.
- Decisões relacionadas: [ADR-0027](0027-modelo-de-musica-hierarquia-artista-album-faixa.md)
  (modelo de dados, inalterado), [ADR-0009](0009-posse-e-status-eixos-ortogonais.md)
  (posse/status ortogonais, base do "faixa desejada"), [AGENTS.md](../../AGENTS.md)
  §4 (enriquecimento opcional e puxado) e §6 (independência de provider).
