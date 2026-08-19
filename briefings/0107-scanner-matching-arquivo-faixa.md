# 0107 — Scanner: matching de arquivo↔faixa (tags de áudio + MusicBrainz)

> Faz o scanner reconhecer arquivo de música, ler tags ID3/Vorbis/MP4 e casar
> automaticamente contra o provider MusicBrainz (0106), fechando o ciclo
> arquivo → faixa catalogada, igual ao que já existe para filme/série.

| | |
|---|---|
| **ID** | 0107 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 5+ — Verticais e plugins (música) |
| **Status** | ready |
| **Docs relacionadas** | [ADR-0027](../docs/adr/0027-modelo-de-musica-hierarquia-artista-album-faixa.md), [06-biblioteca-e-scan](../docs/06-biblioteca-e-scan.md) |
| **Depende de** | 0105, 0106 |

---

## 1. Objetivo

Com 0105 (schema) e 0106 (provider) prontos, falta o elo final: hoje o
scanner **ignora silenciosamente** todo arquivo de áudio (`.mp3`/`.flac`/...)
— cai em "não gerenciado" nas métricas de storage, nunca vira `media_file`
nem entity. Este briefing faz o scan de um `library_source` com
`content_type="music"` **catalogar de verdade** as faixas: ler as tags do
arquivo, casar contra MusicBrainz (por MBID embutido quando existir, por
busca textual quando não) e criar/ligar a hierarquia artista→álbum→faixa —
mesmo padrão de auto-match que filme já tem, adaptado ao vínculo 1:1
(ADR-0027 item 5).

Isso fecha o ciclo prometido pelo pedido original do dono: apontar a pasta
`Musicas` real do Google Drive e ver a biblioteca de música catalogada
sozinha, do mesmo jeito que filme/série já funcionam.

## 2. Jornada do usuário

1. O dono já tem (desde sempre, era no-op até aqui) um `library_source` com
   `content_type="music"` apontando pra pasta de música real (ex.:
   `/media/gdrive-media/Musicas`).
2. Um scan roda (manual ou pelo job periódico já existente). Para cada
   arquivo de áudio: lê as tags, tenta casar contra MusicBrainz.
3. **Caminho feliz — MBID embutido** (arquivo já organizado por
   Picard/beets/similar, tag `MusicBrainz Track Id` presente): casamento
   direto, sem ambiguidade — cria/reaproveita artista→álbum→faixa e linka o
   arquivo.
4. **Caminho feliz — sem MBID embutido** (caso dominante nos scripts pessoais
   do dono, que não gravam esse tag): busca textual no MusicBrainz por
   artista+álbum+título; casamento com confiança alta aplica direto; baixa
   confiança cria a faixa como **rascunho** (mesma filosofia de
   `acharOuCriarDraftSerie`) — nunca bloqueia o scan, nunca inventa match
   errado.
5. Faixa com artista divergente do artista do álbum (feat./colaboração,
   `album_artist` ≠ `artist` na tag) gera a relação `music_artista_feat` além
   de `music_artista_principal` do álbum — validado contra dado real da
   biblioteca do dono (compilação "Various Artists" e álbum colaborativo com
   billing múltiplo, achados durante a validação do 0105).
6. Ao final, as faixas aparecem no grid genérico como qualquer entity
   catalogada — mesma experiência que filme/série já entregam.

## 3. Escopo

**Dentro:**
- Extensões de áudio reconhecidas pelo scanner: `.mp3`, `.flac`, `.m4a`,
  `.ogg`, `.opus`, `.wav` (mesmo padrão de `extensoesVideo`,
  `internal/scanner/scan.go:20-23`, mas separado — `extensoesAudio` novo).
- Extensão de `internal/mediainfo` para expor tags de formato do
  `ffprobe -show_format` (`format.tags`) que hoje são pedidas mas
  descartadas: `artist`, `album`, `album_artist`, `title`, `track`, `disc`,
  `genre`, `TAG:ISRC` (nome real do tag varia por container — mapear
  variações comuns de ID3v2/Vorbis Comment/MP4). Não trocar o wrapper, **só**
  parar de descartar esses campos no `mapear`/`ffFormat`.
- `internal/scanner/musica.go` (novo, molde de `internal/scanner/serie.go`
  mas com vínculo 1:1 como `match.go` de filme):
  - Deriva artista/álbum candidatos das tags (não da estrutura de pasta —
    tags são a fonte de verdade; pasta é só heurística de fallback quando tag
    ausente).
  - Casamento por MBID embutido (fast path) → `ObterEntityPorMbid` (0106) ou
    import direto via `enrich.Servico.Importar(tipo="artista"/"faixa por
    MBID")`.
  - Casamento por busca textual (fallback) via `MusicaProvider`/`Buscar` do
    provider MusicBrainz — critério de confiança simples (normalização de
    string, mesmo espírito de `naming`/matching já usado em filme) definido
    na implementação; documentar o limiar escolhido no briefing atualizado
    ou em comentário no código.
  - Sem match plausível: cria `entity type='faixa'` rascunho (`status='draft'`)
    só com os dados da tag (título, artista como string solta se não achar
    entity — decidir na implementação se cria um artista-rascunho ou deixa
    `music_artista_principal` ausente até correção manual; registrar a
    escolha nas notas de implementação).
  - Persistência 1:1: `media_file.entity_id` aponta direto pra faixa (mesmo
    padrão de filme, `internal/scanner/match.go`) — **não** criar tabela de
    junção nova (ADR-0027 item 5).
- `computarFacts` (`internal/scanner/scan.go:168`) ganha um terceiro ramo
  (hoje só filme/série por `ContentType`) para `content_type == "music"`.
- Extensão de `entity_relation`/`extension_data` reaproveita 100% o que 0105/
  0106 já definiram — nenhuma decisão de modelo nova aqui.

**Fora (explicitamente):**
- UI de revisão dedicada a música (a fila de revisão genérica — 0018/0050 —
  já existe; usar tal como está, sem tela nova específica de música).
- Vínculo N:M arquivo↔faixa (medley/DJ-mix) — adiado no ADR-0027 item 5.
- Auto-match retroativo/job de saúde para música (`internal/job`) — este
  briefing cobre o scan; rodar o job de saúde existente sobre música fica
  para quando fizer sentido validar em produção, sem trabalho novo aqui.
- Watch/fsnotify específico de música — reaproveita o watch genérico já
  existente (0064), sem mudança.

## 4. Contrato técnico

### Dados / modelo
- Nenhuma migration nova. Reaproveita tudo de 0105/0106.

### API
- Nenhum endpoint novo — efeito só aparece via scan (já disparável pela API
  de library existente) e depois pela leitura genérica de entity.

### Backend (Go)
- `internal/mediainfo/mediainfo.go`: expor tags de `format.tags` (struct novo
  ou campo `Tags map[string]string` em `General`, decidir no PR pela forma
  mais simples de não quebrar consumidores existentes de `Info`).
- `internal/scanner/scan.go`: `extensoesAudio`, ramo em `computarFacts` para
  `content_type == "music"`.
- `internal/scanner/musica.go` (+ `_test.go`): pipeline de matching descrito
  acima.
- Reaproveita `internal/enrich` (0106) para o import por MBID/busca textual —
  sem duplicar lógica de criação de artista/álbum já escrita em 0106.

### Frontend (Vite/React)
- Nenhuma mudança neste briefing (grid genérico já exibe qualquer entity).

### Arquivos afetados
- `internal/mediainfo/mediainfo.go`, `_test.go` (editar)
- `internal/scanner/scan.go` (editar — extensões + dispatch)
- `internal/scanner/musica.go`, `_test.go` (novo)
- `internal/scanner/manager.go` (editar, se precisar de nova interface
  estreita tipo `MusicaImportador`, molde de `SerieImporter`/`FilmeImportador`)

## 5. Critérios de aceite

- [ ] `go test ./internal/mediainfo/... -count=1` cobre extração de tags reais
      (fixture de áudio pequena gerada com `ffmpeg`, mesmo padrão do teste de
      integração já existente do pacote) — `artist`/`album`/`album_artist`/
      `track`/`disc`/`genre`/ISRC lidos corretamente.
- [ ] `go test ./internal/scanner/... -count=1` cobre: match por MBID embutido
      (fast path), match por busca textual (fallback, provider fake),
      draft quando sem match, vínculo 1:1 `media_file.entity_id`.
- [ ] Faixa com `artist` de tag divergente de `album_artist` gera
      `music_artista_feat` na faixa mantendo `music_artista_principal` do
      álbum — teste com fixture reproduzindo o caso real achado na validação
      do 0105 ("Various Artists" e o álbum colaborativo multi-artista).
- [ ] Reescanear o mesmo arquivo (mtime/tamanho inalterados) não duplica
      entity nem relação — mesma garantia de incrementalidade que o scan de
      vídeo já tem (`AGENTS.md`, seção de scan/watch).
- [ ] Validação manual em produção (Batocera): rodar scan sobre
      `/media/gdrive-media/Musicas` de verdade, confirmar faixas reais
      catalogadas (ex.: pasta "Duran Duran" e a compilação "Various Artists"
      usadas na validação do 0105) sem erro e sem duplicata.
- [ ] `go build ./...`, `go vet ./...`, `go test ./... -count=1` verdes.
- [ ] `make gate-local` limpo antes do commit final.

## 6. Notas de implementação / decisões

- Toda decisão de modelo (hierarquia, 1:1 arquivo↔faixa, artista-vs-person,
  Various Artists, gênero no álbum) já está fechada no ADR-0027 — este
  briefing só liga o scanner ao que 0105/0106 já construíram. Caso surja algo
  não coberto, parar e voltar para a decisão (mesma regra do 0105/0106).
- Tags são a fonte de verdade para matching (não a estrutura de pasta) porque
  é isso que os scripts pessoais do dono e a documentação de
  Jellyfin/Navidrome/Kodi já assumem — mas a estrutura de pasta
  (`Artista/Álbum (Ano)/Faixa`, inclusive "Disc N" mesmo pra disco único,
  confirmado na validação real do 0105) pode servir de heurística de
  fallback quando a tag vier incompleta.
- Limiar de confiança do fallback textual: definir e documentar no código
  (comentário explícito com o motivo do valor escolhido), não deixar
  implícito — mesmo cuidado que o matching de filme/série já tem.

## 7. Fora deste briefing / follow-ups

- **UI dedicada de música** (tela de artista/álbum, formulário de novo item
  reconhecendo os 3 tipos) — briefing a numerar.
- **Job de saúde/retroativo para música** — avaliar depois de validar o scan
  em produção.
- **Enriquecimento cruzado** (Spotify/Deezer/Discogs/Last.fm) — ADR-0027,
  fica para depois.
