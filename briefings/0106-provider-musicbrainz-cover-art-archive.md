# 0106 — Provider MusicBrainz + Cover Art Archive

> Dá à vertical música (0105) um provider real: busca/import de artista→álbum→
> faixa via MusicBrainz (sem chave) e arte de álbum via Cover Art Archive.

| | |
|---|---|
| **ID** | 0106 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 5+ — Verticais e plugins (música) |
| **Status** | done |
| **Docs relacionadas** | [ADR-0027](../docs/adr/0027-modelo-de-musica-hierarquia-artista-album-faixa.md), [07-enriquecimento-providers](../docs/07-enriquecimento-providers.md), [12-roadmap](../docs/12-roadmap.md) |
| **Depende de** | 0105 |

---

## 1. Objetivo

0105 deu à música os tipos de `entity` e o `extension_data core.music@1`, mas
sem provider a única forma de povoar artista/álbum/faixa é criar entity manual
uma por uma via API — inviável para uma biblioteca real. Este briefing entrega
o provider **MusicBrainz + Cover Art Archive** (ADR-0027 item 6): busca de
artista, import da árvore completa (artista→álbuns→faixas) com metadados reais
e arte de capa por álbum — sem exigir chave de API, mesmo papel que TheTVDB
tem para série.

Depois deste briefing, o dono já consegue **buscar um artista manualmente e
importar a discografia inteira** pela API de busca/import genérica existente
— o scanner/matching automático de arquivo (que casa o que já está na
biblioteca contra isso) é o 0107, separado.

## 2. Jornada do usuário

Este briefing não abre tela nova (a UI de busca/import de música vem depois);
o efeito é observável via a API de busca/import já existente, reaproveitada:

1. O dono chama `GET /api/v1/search?q=Duran+Duran&tipo=artista` — a busca
   inclui resultados do MusicBrainz (nome do artista + MBID).
2. Ele chama `POST /api/v1/import` com o MBID escolhido — o sistema cria a
   entity `artista`, todas as entities `album` (release-groups) e `faixa`
   (recordings) da discografia, já ligadas via `entity_relation`
   (`music_album`, `music_artista_principal`), com título/gênero/numeração
   reais.
3. Cada álbum importado ganha capa (Cover Art Archive) via o mesmo pipeline de
   artes que filme/série já usam (`internal/enrich/artes.go`) — sem código
   novo de exibição.
4. As entities aparecem no grid genérico, navegáveis, com busca FTS — mesma
   validação de "cidadão de 1ª classe" já feita pelo 0105, agora com dado
   real em vez de manual.

## 3. Escopo

**Dentro:**
- Nova capacidade de árvore `provider.MusicaProvider` (molde de
  `SerieProvider`), interface `BuscarArtistaArvore(ctx, sourceID string,
  langs []string) (ArtistaArvore, error)`.
- `internal/provider/musicbrainz/musicbrainz.go`: client HTTP contra
  `https://musicbrainz.org/ws/2/` (JSON, `fmt=json`), implementando:
  - Core `Provider` (`Nome()="musicbrainz"`, `Buscar` com `tipo="artista"` via
    `/artist?query=...`, `BuscarDetalhe`/`MapearDetalhe` mínimos sobre
    `/artist/{mbid}`).
  - `MusicaProvider.BuscarArtistaArvore`: `GET /artist/{mbid}?inc=release-groups`
    para listar álbuns, depois **uma chamada por álbum**
    `GET /release-group/{mbid}?inc=releases` → `GET /release/{mbid}?inc=recordings`
    (ou `inc=recordings+release-groups` numa única chamada por release quando
    possível — decidir na implementação pelo menor nº de requisições) para
    montar faixas com `track_number`/`disc_number`/`disc_total`.
  - `ArteProvider.BuscarArtes`: `GET https://coverartarchive.org/release/{mbid}`
    → mapeia `images[].front==true` para `kind="poster"` do álbum (mesmo shape
    de `provider.Arte` já usado por fanart.tv).
- **Rate limit client-side obrigatório**: MusicBrainz exige ≤ 1 requisição/s e
  um header `User-Agent` identificando a aplicação
  (`MyMediaDB/<versão> (<contato ou URL do projeto>)`) — sem isso a API
  bloqueia por abuso. Nenhum provider existente faz throttle hoje (achado da
  pesquisa); implementar um limitador simples (mutex + timestamp da última
  chamada, sem dependência nova) dentro do próprio pacote
  `internal/provider/musicbrainz`, não um mecanismo genérico em
  `internal/provider` (escopo mínimo — generalizar só se outro provider
  precisar).
- Cover Art Archive **não** tem esse limite documentado, mas usa o mesmo
  client (sem rate-limit adicional).
- **Wiring real em produção**: `provedoresReais()` em `cmd/mymediadb/main.go`
  precisa construir e registrar `musicbrainz.New(...)` incondicionalmente
  (provider sem chave = sempre disponível, mesmo padrão que já deveria valer
  para ThemerrDB — ver achado na seção 6). Critério de aceite cobre isso
  explicitamente porque já existe um precedente real no código de provider
  sem-chave que ficou implementado mas nunca chamado em `main.go`.
- Query sqlc `ObterEntityPorMbid` (espelha `ObterEntityPorTmdb`,
  `internal/db/queries/entity.sql`), usada por 0106 (dedupe de artista/álbum/
  faixa já importados) e por 0107 depois.
- `enrich.Servico`: novo `tipo="artista"` no dispatcher de `Importar`
  (`internal/enrich/enrich.go`), função `importarArtista` que:
  1. Busca ou cria a entity `artista` por MBID (`ObterEntityPorMbid`/criar).
  2. Para cada álbum da árvore: busca ou cria entity `album` por MBID, cria
     `entity_relation(kind=music_album... )` — **atenção**: a relação
     álbum↔faixa é `music_album` (álbum é parent, faixa é child, conforme
     0105/ADR-0027 item 2/4); a relação artista↔álbum é
     `music_artista_principal` (artista parent, álbum child).
  3. Para cada faixa do álbum: busca ou cria entity `faixa` por MBID, grava
     `extension_data core.music@1` (`track_number`, `disc_number`,
     `disc_total`, `isrc` se presente), cria `entity_relation(kind=music_album,
     parent=album, child=faixa, sort_index=track_number)`.
  4. Quando o artista da faixa (MusicBrainz expõe "artist credit" por
     recording) diverge do artista do álbum (release), cria também
     `entity_relation(kind=music_artista_feat, parent=artista_da_faixa,
     child=faixa)` — sem quebrar `music_artista_principal` do álbum.
  5. "Various Artists" (release com artist-credit MusicBrainz = MBID
     `89ad4ac3-39f7-470e-963a-56509c546377`, `musicext.MBIDVariousArtists`)
     é tratado como artista comum — nenhum caso especial de código, só cai
     naturalmente no fluxo acima.

**Fora (explicitamente):**
- Scanner/matching de arquivo local↔faixa (ID3/mediainfo, auto-match no scan)
  — briefing 0107.
- UI dedicada de busca/import de música (formulário, tela de resultado) — a
  API de busca/import genérica já existente serve este briefing; UI fica para
  briefing posterior.
- Enriquecimento cruzado (Spotify/Deezer/Discogs/Last.fm) — ADR-0027,
  "consequências": fica para depois do provider base redondo.
- Corrigir o wiring ausente do ThemerrDB em produção (achado incidental,
  registrado na seção 6 — é um bug separado, não parte deste escopo; abrir
  fix dedicado se confirmado).

## 4. Contrato técnico

### Dados / modelo
- Nenhuma migration nova — reaproveita `entity`/`entity_relation` e a coluna
  gerada `external_id_mbid` (0105).
- Nova query sqlc `ObterEntityPorMbid` (mesmo shape de
  `internal/db/queries/entity.sql:37-44`, `ObterEntityPorTmdb`).

### API
- Nenhum endpoint novo — `GET /api/v1/search?tipo=artista` e
  `POST /api/v1/import` (existentes) passam a aceitar `tipo="artista"` porque
  o novo provider responde a esse `tipo` no dispatch já existente.

### Backend (Go)
- `internal/provider/provider.go`: interface `MusicaProvider` +
  tipos `ArtistaArvore`/`AlbumExterno`/`FaixaExterna` (molde de
  `SerieArvore`/`TemporadaExterna`/`EpisodioExterno`, `provider.go:331-395`).
- `internal/provider/musicbrainz/musicbrainz.go` (+ `_test.go`): client,
  rate limiter, `Provider`/`MusicaProvider`/`ArteProvider`.
- `internal/enrich/musica.go` (novo, molde de `internal/enrich/serie.go`):
  `importarArtista`, dispatch em `enrich.go:175-184`.
- `internal/db/queries/entity.sql`: `ObterEntityPorMbid`.
- `cmd/mymediadb/main.go`: registrar `musicbrainz.New(nil, "")` em
  `provedoresReais()` incondicionalmente (sem `if credencial != ""`, já que
  não tem chave).

### Frontend (Vite/React)
- Nenhuma mudança neste briefing.

### Arquivos afetados
- `internal/provider/provider.go` (editar — nova interface/tipos)
- `internal/provider/musicbrainz/musicbrainz.go`, `_test.go` (novo)
- `internal/provider/musicbrainz/ratelimit.go`, `_test.go` (novo)
- `internal/enrich/musica.go`, `_test.go` (novo)
- `internal/enrich/enrich.go` (editar — dispatch `tipo="artista"`)
- `internal/db/queries/entity.sql`, `internal/db/sqlcgen/*` (editar/gerado)
- `cmd/mymediadb/main.go` (editar — wiring)

## 5. Critérios de aceite

- [ ] `go test ./internal/provider/musicbrainz/... -count=1` cobre: busca de
      artista, montagem de `ArtistaArvore` (fake HTTP server), mapeamento de
      artes via Cover Art Archive, e o rate limiter (nunca dispara 2
      requisições num intervalo < 1s, teste determinístico sem `time.Sleep`
      real longo).
- [ ] `go test ./internal/enrich/... -count=1` cobre `importarArtista`: cria
      artista+álbuns+faixas com `entity_relation` corretos; faixa com artista
      divergente do álbum gera `music_artista_feat` além de
      `music_artista_principal`; reimportar o mesmo MBID não duplica (idempotente
      via `ObterEntityPorMbid`).
- [ ] "Various Artists" importado como artista comum (MBID
      `89ad4ac3-...`), reutilizado em múltiplos álbuns de compilação sem
      duplicar a entity.
- [ ] `provider.Registry.All()` inclui o provider MusicBrainz em produção real
      (teste ou verificação manual pós-deploy contra `cmd/mymediadb/main.go` —
      não repetir o gap do ThemerrDB).
- [ ] Validação manual em produção (Batocera): buscar um artista real da
      biblioteca de música (`/userdata/gdrive/Musicas`) via
      `GET /api/v1/search?tipo=artista`, importar, confirmar hierarquia e
      capa de álbum no banco/API.
- [ ] `go build ./...`, `go vet ./...`, `go test ./... -count=1` verdes.
- [ ] `make gate-local` limpo antes do commit final.

## 6. Notas de implementação / decisões

- **Achado incidental durante a pesquisa deste briefing**: o provider
  ThemerrDB (`internal/provider/themerrdb`, briefing 0103) segue exatamente o
  padrão "sem chave" que este briefing replica, mas **nunca é construído/
  registrado em `provedoresReais()` em produção** — só existe em testes/fakes.
  Se confirmado como bug real (não decisão consciente), o job de tema musical
  em produção está rodando sem provider de verdade. Registrar como achado,
  **não corrigir dentro deste briefing** (fora de escopo/ADR); abrir fix
  dedicado depois de confirmar com o dono.
- Nº de requisições por import: um artista com N álbuns custa ~1 (artista) +
  N (release-group→release) + N (release→recordings) chamadas, a 1 req/s —
  import de discografia grande pode levar minutos. É aceitável (operação
  explícita/assíncrona do dono, não é caminho de leitura), mas documentar no
  código o motivo do rate-limit.
- MusicBrainz devolve `disambiguation` para artistas homônimos — incluir no
  resultado de busca (`ResultadoExterno`) para o dono escolher certo, mesmo
  sem UI dedicada ainda (o campo já existe no shape genérico de resultado).
- Se durante a implementação surgir um caso de hierarquia não coberto pelo
  ADR-0027 (ex.: release-group sem nenhuma release "oficial", faixa sem
  artist-credit), **parar e voltar para a decisão** em vez de inventar
  solução ad-hoc — mesma regra do 0105.

## 7. Fora deste briefing / follow-ups

- **Scanner/matching de arquivo↔faixa** — briefing 0107 (próximo, já
  numerado).
- **UI de busca/import de música** — briefing a numerar depois do 0107.
- **Enriquecimento cruzado** (Spotify/Deezer/Discogs/Last.fm) — fora até o
  provider base estar redondo (ADR-0027).
- **Fix do wiring ausente do ThemerrDB** — achado incidental, fica para um
  fix separado se confirmado.
