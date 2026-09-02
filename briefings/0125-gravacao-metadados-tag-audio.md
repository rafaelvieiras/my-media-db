# 0125 — Gravação de metadados resolvidos na tag de áudio (write-back)

> Em vez de um `.nfo` de música (que só o Kodi lê), o MyMediaDB regrava a
> própria tag do arquivo (ID3v2/Vorbis comment) com o metadado já RESOLVIDO —
> título, artista, álbum, ano, faixa/disco, gênero, MBIDs, ISRC e capa
> embutida — para que QUALQUER player/servidor (LMS, Navidrome, gonic,
> Supysonic, clientes Subsonic/OpenSubsonic, Kodi em modo tag-scan, Jellyfin)
> leia a biblioteca corretamente sem depender de NFO nenhum.

| | |
|---|---|
| **ID** | 0125 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 5+ — Música (artista/álbum/faixa) |
| **Docs relacionadas** | [08-artwork-e-assets](../docs/08-artwork-e-assets.md), [ADR-0027](../docs/adr/0027-modelo-de-musica-hierarquia-artista-album-faixa.md), [ADR-0029](../docs/adr/0029-import-de-musica-local-first-por-faixa-possuida.md) |
| **Status** | done |
| **Depende de** | 0105, 0106, 0107, 0115 |

---

## 1. Objetivo

NFO de música **não existe funcionalmente** no MyMediaDB hoje: a UI desabilita
"Salvar informações (NFO)"/"Salvar na pasta" para `type==='album'` e
`type==='artista'` (`web/src/routes/entity-detail.tsx`, comentário em torno da
linha 266-273 — `podeGravarNaPastaReal`), porque `resolverDestino` não sabe
escolher UMA pasta entre as várias faixas que compõem um álbum/artista
(artista/álbum nunca têm `media_file` próprio, ADR-0027). E mesmo que fosse
habilitado para `type==='faixa'`, `internal/localmedia/nfo.go` (`nomeRaizNFO`)
só reconhece `TipoSerie→tvshow`/`TipoEpisodio→episodedetails` e cai no
`default: movie` para qualquer outro tipo — produziria um `.nfo` com raiz
`<movie>` errada para uma faixa.

Investigação de mercado (comparando com o que o dono já roda em produção
sobre a mesma biblioteca) confirmou que consertar isso não vale o
investimento: **NFO de música só é relevante para o Kodi**, e de forma rasa/
limitada mesmo para o Jellyfin. Todo o resto do ecossistema de consumo de
música que importa aqui — **LMS/Lyrion** (rodando em produção sobre
`/userdata/gdrive/Musicas`, mesma biblioteca, ver ADR-0029), **Navidrome**,
**gonic**, **Supysonic** e, por extensão, praticamente todo cliente Subsonic/
OpenSubsonic (DSub, Substreamer, Symfonium, play:Sub) — lê metadado **direto
da tag de áudio embutida no arquivo** (ID3v2 em MP3, Vorbis comments em
FLAC/OGG), nunca de um `.nfo` ao lado.

O MyMediaDB hoje só **lê** tag (via ffprobe, `internal/mediainfo/mediainfo.go`
`AudioTags`/`extrairAudioTags`) — nunca regrava nada no arquivo de áudio. Este
briefing fecha esse gap pela ponta que realmente importa: em vez de produzir
um `.nfo`, o sistema **regrava a tag do próprio arquivo** com o metadado
resolvido (o mesmo valor que já passou pelo resolver de 3 camadas `override
⟵ externo ⟵ arquivo` e que a tela já exibe) — "aquecendo" o arquivo para
qualquer consumidor externo, sem depender de nenhum software de terceiro
entender NFO.

## 2. Jornada do usuário

1. O dono importa/enriquece um álbum (ex.: via MusicBrainz, briefing 0106) —
   ou já tinha faixas possuídas casadas via scanner (briefing 0107/0115).
2. Na tela do álbum, o metadado resolvido aparece normalmente (título, artista,
   ano, capa, tracklist com número/disco) — igual hoje.
3. No menu "Gerenciar" do álbum (ou de uma faixa individual), o dono vê um
   novo item **"Gravar metadados no arquivo"**.
4. Ao clicar, um diálogo de confirmação explica o efeito: a ação **sobrescreve
   a tag original do(s) arquivo(s) de áudio** com o valor atualmente resolvido
   — inclusive a capa embutida — e isso é, na prática, irreversível (não há
   "desfazer": o valor anterior da tag não é preservado em lugar nenhum).
5. Ao confirmar:
   - Se disparado a partir de uma **faixa**, regrava só o arquivo daquela
     faixa.
   - Se disparado a partir de um **álbum**, regrava a tag de **todas as
     faixas possuídas** daquele álbum (uma por uma), sempre com o valor
     resolvido de cada faixa individualmente (título/número/disco/ISRC
     variam por faixa; artista do álbum/ano/gênero/capa são os mesmos para
     todas, exceto quando uma faixa tem artist-credit divergente —
     feat./colaboração, ADR-0027 item 4 — caso em que a tag daquela faixa
     leva o artista da PRÓPRIA faixa, não o do álbum).
6. O sistema mostra progresso/resultado ("N faixa(s) regravada(s), M com
   erro") no mesmo padrão de feedback dos demais itens do menu Gerenciar.
7. Reabrindo o arquivo em qualquer player/servidor externo (LMS, Navidrome,
   Kodi em modo tag-scan…), a tag já reflete o metadado do MyMediaDB.

## 3. Escopo

**Dentro:**
- Ação explícita "Gravar metadados no arquivo" para `type==='faixa'` (grava
  só aquele arquivo) e `type==='album'` (grava todas as faixas possuídas do
  álbum, uma a uma).
- Escrita dos campos: título, artista (da faixa, respeitando divergência
  feat./ADR-0027 item 4), album artist, álbum, ano/data de lançamento,
  número de faixa/total de faixas, número de disco/total de discos, gênero,
  flag de compilation (quando o álbum estiver ligado a "Various Artists",
  MBID `musicext.MBIDVariousArtists`), MusicBrainz IDs (recording/
  release/release-group/artist), ISRC, e a capa do álbum embutida
  (APIC em ID3v2 / METADATA_BLOCK_PICTURE em Vorbis comment) — sempre o
  valor RESOLVIDO (já passou por `override ⟵ externo ⟵ arquivo`), nunca um
  valor bruto de provider ignorando override do dono.
- Confirmação explícita (diálogo, mesmo padrão `AlertDialog` já usado em
  `entity-manage-menu.tsx` para excluir coleção) antes de executar, dado o
  caráter destrutivo/irreversível sobre o arquivo original.
- Atualizar `media_file.hash`/`mtime`/`size` da faixa regravada no mesmo
  golpe (reaproveitando a capacidade de recheck restrito já existente desde
  o briefing 0088 — `internal/scanner`, função de re-hash/mediainfo por path
  específico), para o próximo scan não encontrar uma "mudança" inesperada.
- Suporte a MP3 (ID3v2) e FLAC/OGG (Vorbis comment) — os formatos que os
  scripts pessoais do dono e a biblioteca real já usam (mesma fonte empírica
  citada no ADR-0027).

**Fora (explicitamente):**
- **NFO de música.** Decisão tomada: não vale o investimento — cobre só
  Kodi (e de forma rasa no Jellyfin), enquanto LMS/Navidrome/gonic/Supysonic
  e o ecossistema Subsonic/OpenSubsonic são tag-based. `nomeRaizNFO`
  (`internal/localmedia/nfo.go`) continua sem conhecer `faixa`/`album`/
  `artista` — não é um gap a fechar, é escopo abandonado.
- **Dados de prosa em nível artista** (biografia, formado/dissolvido,
  disambiguation) **e em nível álbum** (texto de review): não têm "arquivo
  dono" — tag é por FAIXA, não existe um arquivo do artista/álbum para
  gravar isso. Ficam só no banco, resolvidos pelas telas do app. Limitação
  aceita, não pendência.
- **Ação para `type==='artista'`.** Sem granularidade de "obra" coerente
  (cascatear para todas as faixas de todos os álbuns do artista é escopo
  maior e mais arriscado — fica para follow-up se o dono sentir falta).
- **Leitura/parse de volta da tag regravada para o banco.** A escrita é
  **one-way**: banco → arquivo, nunca relida — mesma filosofia do NFO de
  filme/série (`AGENTS.md`: "NFO é exportação one-way... nunca é relido além
  do id determinístico que o scanner já usa"). Depois de regravada, a tag do
  arquivo deveria bater com o que a UI mostra, mas o MyMediaDB nunca volta a
  ler esse valor de volta como fonte — a camada "arquivo" do resolver
  continua sendo alimentada pelo próximo `ffprobe`, não por este write-back.
- **Gatilho automático** no scan/import/enriquecimento retroativo. Nunca
  roda sozinho — só a partir do clique explícito do dono no menu Gerenciar
  (mesmo padrão das 3 ações forçadas do briefing 0088), preservando o
  princípio "não-destrutivo" (AGENTS.md §5): regravar tag é uma escrita
  destrutiva no arquivo original do dono, então exige ação e confirmação
  explícitas, nunca acontece "nas costas dele".
- **Materializar a arte na pasta como arquivo separado** (poster.jpg/
  folder.jpg) — isso já existe via NFO/artes locais de filme/série e via
  0119 para descoberta; este briefing só embute a capa DENTRO da tag do
  áudio, não mexe no mecanismo de arquivo de capa solto na pasta.
- **Cascata em lote para múltiplos álbuns/toda a biblioteca de um artista**
  de uma vez — só álbum→faixas ou faixa individual nesta entrega.

## 4. Contrato técnico

### Dados / modelo
- Nenhuma migração de schema é estritamente necessária: o campo escrito é
  sempre o já resolvido, lido via o mesmo caminho que a tela de detalhe usa
  (`resolver.Entidade` + `internal/musicext` para `track_number`/
  `disc_number`/`disc_total`/ISRC do namespace `core.music@1`).
- Reaproveitar a query/capacidade já existente do briefing 0088
  (`internal/scanner`, recheck restrito a paths específicos) para atualizar
  `media_file.hash`/`mtime`/`size` após a escrita — sem query nova, é a
  mesma função reaproveitada a partir de um novo caller.

### API
- `POST /api/v1/entities/{id}/audio-tags/write` — sem body. `id` pode ser
  uma `faixa` (grava só ela) ou um `album` (grava todas as faixas
  possuídas). 404 se a entity não existe; 400/422 se `type` não for
  `faixa`/`album` (ex.: `artista` — fora de escopo, ver §3); resposta 200
  com um resumo (ex.: `{"faixas_regravadas": N, "falhas": [...]}`, moldar
  conforme a implementação — mesmo espírito informativo do 0088, não
  precisa ser rígido).
- Atualizar `openapi.yaml` com o path novo.

### Backend (Go)
- Pacote novo provável: `internal/audiotag` (nome sugerido — decisão de
  nomenclatura fica para quem implementar; alternativa é um sub-pacote
  dentro de `internal/localmedia`, mas como esta feature NÃO usa NFO, um
  pacote próprio evita acoplar a algo que este briefing explicitamente
  descarta). Responsabilidade: dado um path de arquivo + o metadado
  resolvido (struct própria, moldada a partir de `resolver.Entidade` +
  `musicext`), regravar a tag no formato certo (ID3v2 para `.mp3`, Vorbis
  comment para `.flac`/`.ogg`) — puro em relação ao resolver (recebe valores
  já resolvidos, não decide precedência).
- Handler novo em `internal/server` (ex. `audio_tags.go`), registrado em
  `server.go` junto das demais rotas de `/entities/{id}/...` — mesmo padrão
  dos handlers do briefing 0088 (`internal/server/refresh_files.go` como
  referência de forma: resolve a(s) faixa(s) via `ListarMediaFilesPorEntity`
  ou equivalente, monta o metadado resolvido via o mesmo caminho que
  `resolverEntity` usa, chama o pacote de escrita, e no final aciona o
  recheck restrito do 0088 para sincronizar `media_file`).
- Para `type==='album'`: iterar as faixas possuídas (mesma query/CTE de
  árvore já usada por `ListarEntitiesMusicaComFaixaPossuida`/
  `ObterCaminhoDescendentePresente`, briefing 0119) e chamar a escrita por
  faixa, best-effort (uma falha isolada não aborta as demais — reportar no
  resumo).
- Resolver o valor de artista por faixa respeitando divergência feat.
  (ADR-0027 item 4): se a faixa tem `entity_relation(kind=
  music_artista_feat)` ou as chaves `feat_artist_name`/`feat_artist_mbid`
  em `core.music@1` (`internal/musicext`), a tag de ARTISTA daquela faixa
  usa o nome do artista da própria faixa, não o artista principal do álbum
  (que continua indo em `AlbumArtist`).

### Frontend (Vite/React)
- `web/src/lib/api.ts`: função nova `writeAudioTags(id)`, mesmo padrão de
  `refreshEntityFiles`/`materializeMissingMedia` (0088).
- `web/src/components/entity-manage-menu.tsx`: item novo "Gravar metadados
  no arquivo", visível quando `type==='faixa' || type==='album'` (não
  depende de `podeGravarNaPastaReal`/posse de pasta real, que é sobre NFO —
  esta ação não precisa de uma pasta única, só dos arquivos já vinculados).
  `AlertDialog` de confirmação (reaproveitar o padrão já usado no mesmo
  arquivo para excluir coleção — `alertaAberto`/`AlertDialogContent`/
  `AlertDialogAction`) alertando que a operação sobrescreve a tag original e
  é irreversível na prática. `useMutation` com feedback de
  carregando/sucesso/erro, mesmo padrão dos itens do 0088.
- Ícone sugerido: `Tag` ou `FileAudio` (lucide-react, conferir disponível no
  pacote já instalado).

### Arquivos afetados
- `internal/audiotag/*.go` (novo pacote) + testes.
- `internal/server/audio_tags.go` (novo handler) + teste, `internal/server/server.go`
  (rota nova).
- `internal/scanner/*.go` — reaproveitado (recheck restrito do 0088), sem
  mudança esperada além de um novo caller.
- `internal/musicext/musicext.go` — reaproveitado para ler `track_number`/
  `disc_number`/`disc_total`/ISRC; sem mudança esperada.
- `openapi.yaml`.
- `web/src/lib/api.ts`, `web/src/components/entity-manage-menu.tsx` (+
  teste), tipos gerados (`gen:types`).

## 5. Critérios de aceite

- [x] `POST /entities/{id}/audio-tags/write` com `id` de uma `faixa` regrava
      a tag do arquivo vinculado com título/artista/álbum/ano/faixa-disco/
      gênero/MBIDs/ISRC/capa resolvidos, e sincroniza `media_file.hash`/
      `mtime`/`size` no mesmo golpe. Confirmado por
      `TestWriteAudioTagsFaixaAvulsaRegravaTagESincronizaMediaFile`
      (`internal/server/audio_tags_test.go`), que roda o ffmpeg de verdade,
      relê a tag via `ffprobe` e confere `content_hash` mudou pós-escrita.
- [x] Mesmo endpoint com `id` de um `album` regrava a tag de TODAS as faixas
      possuídas daquele álbum, cada uma com seu próprio
      título/número/disco/ISRC, mas artista/ano/gênero/capa do álbum
      compartilhados (exceto quando a faixa tem artist-credit divergente —
      nesse caso a tag de artista daquela faixa é a do artist-credit da
      própria faixa, não o artista principal do álbum). Confirmado por
      `TestWriteAudioTagsResolveArtistaFeatNaTag` (artist/album_artist
      divergentes na tag real).
- [x] Uma falha isolada ao regravar UMA faixa do álbum não aborta as
      demais — o resumo da resposta reporta sucesso parcial. Confirmado por
      `TestWriteAudioTagsAlbumCascataSucessoParcial`.
- [x] `id` de `type==='artista'` retorna erro (ação não suportada), sem
      tentar cascatear. Confirmado por `TestWriteAudioTagsTypeArtistaDevolve422`
      (e `TestWriteAudioTagsTypeMovieDevolve422` para qualquer outro tipo).
- [x] Suporta MP3 (ID3v2) e FLAC/OGG (Vorbis comment). Confirmado pelos
      testes reais de `internal/audiotag/writer_real_test.go`
      (`TestRealFlacEOggEscrevemTagsECapa` + o caminho MP3 dos testes de
      `audio_tags_test.go` acima).
- [x] A escrita nunca é disparada implicitamente por scan/import/
      enriquecimento — só pelo endpoint acima, chamado explicitamente
      (único caller de `audiotag.Writer.Escrever` é `writeAudioTags`,
      confirmado por leitura de código).
- [x] O item "Gravar metadados no arquivo" aparece no menu Gerenciar para
      `faixa`/`album`, com diálogo de confirmação antes de executar e
      feedback de carregando/sucesso/erro. Confirmado em
      `web/src/components/entity-manage-menu.tsx`
      (`mostrarGravarTag`/`AlertDialog`/`gravarTag` mutation) e testado em
      `entity-manage-menu.test.tsx`.
- [x] A tag regravada NUNCA é relida de volta para popular o banco (a
      camada "arquivo" do resolver segue vindo só do próximo `ffprobe`
      natural, não deste write-back). Confirmado por leitura de código:
      `internal/audiotag` não expõe nenhuma função de leitura, só
      `Escrever`.
- [x] `go build ./... && go vet ./... && go test ./... -count=1` verdes.
- [x] `tsc -b` verde, `npm run lint` verde, `npm run test:coverage` verde.
- [x] `make gate-local` sem regressão (cobertura Go e Web subiram, 0
      issues de lint/type-check/vulns; a única métrica que subiu é gocyclo
      >10, aviso não-bloqueante por AGENTS.md).

## 6. Notas de implementação / decisões

- **Por que abandonar NFO de música em vez de consertar `nomeRaizNFO` +
  `resolverDestino`?** Pesquisa de mercado mostrou que NFO de música só
  serve ao Kodi (raso mesmo no Jellyfin); o resto do ecossistema de consumo
  real do dono (LMS em produção, Navidrome, gonic, Supysonic, clientes
  Subsonic/OpenSubsonic) é tag-based. Investir em resolver "qual é a pasta
  de um álbum/artista com várias faixas" para produzir um arquivo que quase
  nada lê não compensa; regravar a tag cobre TODOS esses consumidores de
  uma vez, incluindo o Kodi (que também sabe ler tag em modo "tag-scan",
  ainda que prefira NFO quando presente).
- **Por que a escrita é sempre sobre o valor RESOLVIDO, nunca muda a
  precedência do resolver?** O resolver continua `override ⟵ externo ⟵
  arquivo` (AGENTS.md §1); esta feature só faz a camada "arquivo" (a de
  MENOR prioridade) alcançar, fisicamente no disco, o valor que as camadas
  de cima já decidiram. Não é um caminho de escrita alternativo — é
  materializar a decisão já tomada.
- **Efeito colateral sobre o scan incremental (AGENTS.md, "Biblioteca em
  disco: scan/watch"):** regravar a tag necessariamente muda `mtime`/
  tamanho do arquivo. Sem tratamento, o próximo scan incremental veria o
  arquivo como "mudado" e re-hashearia/re-probaria — comportamento
  ESPERADO do mecanismo existente, não um bug. Decisão: em vez de aceitar
  esse re-hash "de surpresa", o endpoint de escrita já atualiza
  `media_file.hash`/`mtime`/`size` no mesmo golpe (reaproveitando o recheck
  restrito do briefing 0088), para o próximo scan já ver o arquivo como
  íntegro/atual. Quem implementar deve confirmar que essa atualização
  reflete o mtime/tamanho REAIS pós-escrita (não os valores antigos).
- **Biblioteca/mecanismo Go de escrita — decisão de implementação em
  aberto.** O projeto hoje só LÊ tag via `ffprobe` (subprocesso,
  `internal/mediainfo`); não há lib de ESCRITA de tag em uso. Opções a
  pesquisar na hora de implementar: (a) uma lib Go dedicada de leitura/
  escrita de ID3v2 (ex. no espaço `github.com/bogem/id3v2` ou similar) +
  uma lib de Vorbis comment separada para FLAC/OGG (ex.
  `github.com/go-flac/go-flac` + pacote de metadata, a conferir
  disponibilidade/manutenção atual); (b) invocar `ffmpeg` como subprocesso
  para regravar metadata (`-map_metadata`/`-metadata key=value` + remux),
  que já resolveria MP3 e FLAC/OGG com uma única ferramenta (o projeto já
  depende de ffmpeg/ffprobe em runtime, então não introduz dependência
  binária nova) mas historicamente é mais delicado para embutir capa
  (APIC/METADATA_BLOCK_PICTURE) corretamente sem remux completo do áudio.
  Este briefing não decide entre (a) e (b) — só levanta a pergunta e as
  opções conhecidas para quem for implementar já saber o que avaliar
  (licença, manutenção ativa, suporte a picture frame, custo de remux vs.
  edição in-place).
- **Por que álbum cascateia para as faixas, ao contrário do padrão "sem
  cascata automática" do briefing 0088?** No 0088, cada ação vale só para a
  entity aberta porque ELA tem arquivo/pasta própria. Álbum nunca tem
  arquivo próprio (ADR-0027) — "gravar metadados do álbum" só faz sentido
  operacionalmente como "gravar em cada faixa que o compõe". Não é uma
  cascata em massa arbitrária (tipo "todos os álbuns do artista"), é a
  única interpretação coerente da ação no nível em que foi disparada.

## 7. Fora deste briefing / follow-ups

- Ação equivalente disparável a partir de `type==='artista'` (cascata sobre
  todos os álbuns/faixas do artista) — se o dono sentir falta depois de
  usar a versão por álbum/faixa.
- Escolha e integração efetiva da biblioteca/mecanismo de escrita de tag
  (ver nota acima) — este briefing só levanta a decisão, não a resolve.
- Qualquer UI de "preview" mostrando o que vai mudar na tag antes de
  confirmar (o diálogo de confirmação desta entrega é textual/genérico, não
  um diff campo a campo).
- Suporte a formatos de áudio além de MP3/FLAC/OGG, se a biblioteca real do
  dono precisar (ex. M4A/AAC) — avaliar quando/se aparecer caso real.
