# 0102 — Trailer: materialização em disco (TMDB/TVDB `/videos` + yt-dlp)

> Baixa o trailer já listado hoje (player embutido de YouTube, briefing 0086)
> pro arquivo local `<nome-canônico>-trailer.<ext>`, convenção Kodi/Jellyfin.

| | |
|---|---|
| **ID** | 0102 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 5+ — Trailer e música-tema |
| **Status** | done |
| **Docs relacionadas** | [ADR-0025](../docs/adr/0025-trailer-tema-musical-ytdlp-themerrdb.md), [08-artwork-e-assets](../docs/08-artwork-e-assets.md) |
| **Depende de** | 0101 |

---

## 1. Objetivo

`GET /entities/{id}` já expõe `videos` (`richfields.Video`, extraído do TMDB) e
o frontend já toca o trailer embutido via iframe do YouTube (`TrailerModal`,
`web/src/components/rich-detail-view.tsx:355-408`, briefing 0086/0082). O que
falta é **materializar esse trailer no disco**, convenção Kodi/Jellyfin, para
quem usa o MyMediaDB só como catalogador de uma biblioteca consumida por outro
media center. O player embutido continua existindo e funcionando igual, com ou
sem download local — este briefing é puramente aditivo.

## 2. Jornada do usuário

1. Na tela de detalhe de um item com trailer disponível (`primeiroTrailer`
   não-nulo), ao lado do botão "Assistir trailer" existente aparece "Baixar
   trailer" — só quando o yt-dlp está disponível (`GET /settings/ytdlp`,
   0101); ausente/indisponível, o botão simplesmente não aparece (sem erro,
   sem estado quebrado — o "Assistir trailer" que já existe continua igual).
2. Clique dispara `POST /entities/{id}/trailer/download`; UI mostra estado
   "baixando…" (o download é síncrono nesta entrega — ver §6) e depois
   "trailer baixado" com um link para o arquivo/pasta.
3. Reabrir a tela depois: o botão mostra "trailer já baixado" (estado
   persistente, calculado a cada `GET`, sem campo novo no banco — ver §4).
4. Falha (rede, vídeo removido do YouTube, yt-dlp indisponível no meio):
   mensagem de erro clara, nada é escrito no disco (escrita atômica —
   reaproveita `localmedia.EscreverArquivoAtomico`/mesmo princípio do NFO).
5. Item sem trailer nenhum (`primeiroTrailer` nulo): nenhum botão de download
   aparece — igual ao comportamento atual do "Assistir trailer".

## 3. Escopo

**Dentro:**
- `internal/localmedia/naming.go`: `KindTrailer = "trailer"` (novo const) +
  entrada em `extPadraoDeKind` (`.mp4` — extensão default de vídeo; o nome
  final real usa a extensão que o yt-dlp efetivamente baixar). `baseComPrefixo`
  **não precisa mudar** — kind desconhecido já cai em `base = kind` (comentário
  em `naming.go:100` já antecipa exatamente este caso), produzindo
  `<nomeBaseVideo>-trailer.<ext>` (pasta não-exclusiva) ou `trailer.<ext>`
  (pasta exclusiva) — convenção Kodi correta nos dois casos, zero mudança de
  regra.
- **Sem tabela `asset` nova/kind novo em `internal/server/assets.go`** — trailer
  NÃO entra no pipeline de upload/galeria de imagem (`parseKind`,
  `escolherAssetServido`, proxy de imagem: tudo assume imagem, não vídeo).
  Segue o precedente do `theme.mp3` (briefing 0082): detecção por
  existência de arquivo na pasta resolvida, sem linha de banco dedicada.
- `internal/server/localmedia_handlers.go`: novo bloco espelhando a seção do
  tema (linhas 614-700) — `encontrarArquivoTrailer(dir, nomeBaseVideo,
  pastaExclusiva)` (usa `localmedia.NomeArquivoArte(localmedia.KindTrailer, ...)`
  pra saber o nome esperado, testando as extensões de vídeo comuns que o
  yt-dlp pode produzir: `.mp4`, `.mkv`, `.webm`).
- `GET /api/v1/entities/{id}/trailer` — detecta e serve o binário local (mesmo
  padrão de `getEntityTheme`), 404 se ausente.
- `POST /api/v1/entities/{id}/trailer/download` — body opcional
  `{"key": "<youtube-key>"}` (default: primeiro `Video` com
  `type=="Trailer" && site=="YouTube"`, mesma regra de `primeiroTrailer` do
  frontend — reimplementar em Go sobre `detail.videos`/`richfields.VideosDe`).
  Resolve `dir`/`nomeBaseVideo`/`pastaExclusiva` via `s.resolverDestino` (já
  usado por artwork/nfo/tema), monta o destino com
  `localmedia.NomeArquivoArte(localmedia.KindTrailer, true, 0, pastaExclusiva, nomeBaseVideo, "")`
  e chama `s.ytdlp.Baixar(ctx, urlDoVideo, destinoTemp)` (0101) — escreve em
  arquivo temporário e RENOMEIA pro destino final só em caso de sucesso
  (nunca deixa arquivo parcial com o nome canônico). **Nunca sobrescreve** um
  trailer já presente (princípio 5) — 409/no-op se já existe, a menos que o
  chamador peça explicitamente re-download (parâmetro `force`, opcional).
  Sem yt-dlp disponível (`s.ytdlp.Status`): 503 com mensagem clara, nunca 500.
- Frontend: `TrailerButton` (`rich-detail-view.tsx:392-409`) ganha um segundo
  botão condicional "Baixar trailer" — consulta `GET /settings/ytdlp` (cache
  de app, não por-item) e o estado local do trailer (via um campo novo na
  resposta de `GET /entities/{id}`, ver §4, ou uma checagem HEAD/GET separada
  a `/trailer` — decidir na implementação pelo custo menor).

**Fora (explicitamente):**
- Download assíncrono/em fila com progresso incremental — este briefing é
  síncrono (o request HTTP fica pendurado até terminar); mover pro sistema de
  jobs (`internal/jobs`, briefing 0052) fica de follow-up se o trailer médio
  demorar demais na prática.
- Baixar teaser/clipe/featurette além do trailer principal — só `type="Trailer"`
  nesta entrega; outros tipos ficam de follow-up (o seletor por `key` já deixa
  a porta aberta pro frontend oferecer escolha futura).
- TVDB como fonte de trailer (só TMDB nesta entrega — o TVDB tem o campo mas
  não é consumido hoje por `richfields.VideosDe`; ver se isso já cobre o
  necessário antes de estender).
- Qualquer UI de "gerenciar trailers baixados" tipo galeria — é só o botão +
  estado simples.

## 4. Contrato técnico

### Dados / modelo
- Nenhuma migration. Estado é 100% derivado do filesystem (igual ao tema).
- `GET /entities/{id}` pode ganhar um campo opcional `trailer_local: bool`
  (calculado on-demand via `encontrarArquivoTrailer`) para o frontend não
  precisar de uma chamada extra — decidir custo (I/O por `stat` é barato,
  mas acontece em TODO `GET /entities/{id}`; medir antes de aceitar, senão
  manter como chamada separada só quando a tela de detalhe abre).

### API
- `GET /api/v1/entities/{id}/trailer` → binário (200) ou 404.
- `POST /api/v1/entities/{id}/trailer/download` body `{"key"?: string, "force"?: bool}` →
  `{"local_path": string}` (200) | 503 (`sem yt-dlp disponível`) | 409 (já existe, sem `force`) | 422 (sem trailer YouTube disponível pro item).
- Atualizar `openapi.yaml` (novo bloco perto de `/entities/{id}/theme`, linha ~2049 região de webhooks não — buscar a seção `/entities/{id}` no arquivo).

### Backend (Go)
- `internal/localmedia/naming.go`: `KindTrailer` + `extPadraoDeKind[KindTrailer] = ".mp4"`.
- `internal/server/localmedia_handlers.go`: `encontrarArquivoTrailer`, `getEntityTrailer`, `postEntityTrailerDownload`.
- `internal/richfields` ou `internal/server/external_detail.go`: função `primeiroTrailerYoutube(videos []richfields.Video) (richfields.Video, bool)` reaproveitável pelo backend (espelha `primeiroTrailer` do frontend, `rich-detail-view.tsx:351-353`) — evita duplicar a regra em dois lugares/duas linguagens sem uma fonte comum documentada.

### Frontend (Vite/React)
- `rich-detail-view.tsx`: `TrailerButton` ganha estado de download; novo hook/query pro status do yt-dlp (`GET /settings/ytdlp`, cacheado via TanStack Query, staleTime alto — não é dado que muda por item).

### Arquivos afetados
- `internal/localmedia/naming.go`, `internal/server/localmedia_handlers.go`, `internal/server/external_detail.go` (ou `richfields`), `openapi.yaml`, `web/src/components/rich-detail-view.tsx`, `web/src/lib/api-types.ts` (gerado).

## 5. Critérios de aceite

- [x] `localmedia.NomeArquivoArte(localmedia.KindTrailer, true, 0, false, "Filme (2026)", ".mp4")` produz `"Filme (2026)-trailer.mp4"`; com `pastaExclusiva=true` produz `"trailer.mp4"`. (`internal/localmedia/naming_test.go: TestNomeArquivoArteTrailer`)
- [x] `POST /trailer/download` sem trailer YouTube no item → 422, sem tocar disco. (`internal/server/trailer_test.go: TestPostTrailerDownloadSemTrailer422`)
- [x] `POST /trailer/download` com yt-dlp indisponível → 503, sem tocar disco. (`internal/server/trailer_test.go: TestPostTrailerDownloadSemYtdlp503`)
- [x] `POST /trailer/download` bem-sucedido grava no path canônico e é idempotente (segunda chamada sem `force` → 409, arquivo original intacto). (`internal/server/trailer_test.go: TestPostTrailerDownloadSucessoIdempotenteENuncaSobrescreve`)
- [x] `GET /trailer` serve o binário existente com content-type coerente com a extensão; 404 sem arquivo. (`internal/server/trailer_test.go: TestGetEntityTrailerServeBinario`, `TestGetEntityTrailerSemArquivo404`, `TestGetEntityTrailerSemMediaFile404`)
- [x] Item com trailer já colocado manualmente pelo dono (mesmo nome canônico) NUNCA é sobrescrito sem `force=true` explícito. (`internal/server/trailer_test.go: TestPostTrailerDownloadTrailerManualNuncaSobrescrito`)
- [x] `go build ./... && go vet ./... && go test ./... -count=1`; `tsc -b`/lint/`test:coverage` verdes. Validado também com `make gate-fast` (cobertura Go e Web SUBIRAM frente ao baseline).

## 6. Notas de implementação / decisões

- **Download síncrono (não job em background) nesta entrega**: simplifica a
  primeira versão; trailers costumam ser MB baixos (dezenas de MB, não GB) —
  aceitável travar o request por alguns segundos/poucos minutos. Reavaliar
  pra `internal/jobs` se a prática mostrar timeout de proxy/UI travada.
- **Sem tabela `asset`**: decisão explícita para não misturar vídeo no
  pipeline de imagem (`parseKind`/galeria/proxy assumem JPEG/PNG). Segue o
  precedente já existente do `theme.mp3` (0082), que também não usa `asset`.
- Escrita sempre atômica (temp + rename) — mesma disciplina de
  `localmedia.EscreverArquivoAtomico`/`RenomearArquivo`, adaptada pro yt-dlp
  escrever direto no destino temporário (`-o <temp>`) e o Go renomear ao final.

## 7. Fora deste briefing / follow-ups

- Download assíncrono via `internal/jobs`.
- Teaser/clipe/featurette além de Trailer.
- TVDB como fonte adicional de vídeo.
